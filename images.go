package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	triton "github.com/joyent/triton-go"
	"github.com/joyent/triton-go/authentication"
	"github.com/joyent/triton-go/compute"
)

type ImageDBSession struct {
	mutex sync.Mutex
	db    map[string]*compute.Image

	pool   *ImageWorkerPool
	client *compute.ImagesClient
}

type ImageQueryInfo struct {
	ID       string
	Receiver chan *ImageQueryFuture
}

type ImageWorkerPool struct {
	tries   map[string]int
	working map[string]bool
	waiting map[string]chan struct{}

	workerInput chan *ImageQueryInfo

	queryInput chan string

	jobs    sync.WaitGroup
	workers sync.WaitGroup
}

type ImageQueryFuture struct {
	ID      string
	Done    chan struct{}
	Value   *compute.Image
	Session *ImageDBSession
	Error   error
}

func (f *ImageQueryFuture) String() string {
	return fmt.Sprintf("{ ID: %s, Done: %v }", f.ID, f.Done)
}

func (f *ImageQueryFuture) Get() (*compute.Image, error) {
	select {
	case <-f.Done:
	}
	f.Session.mutex.Lock()
	defer f.Session.mutex.Unlock()
	f.Value = f.Session.db[f.ID]
	return f.Value, nil
}

func NewImageSession(client *compute.ImagesClient, nworkers int) *ImageDBSession {
	s := ImageDBSession{db: make(map[string]*compute.Image), client: client,
		pool: newImageWorkerPool(nworkers)}

	for i := 0; i < nworkers; i++ {
		s.pool.workers.Add(1)
		go s.Worker(i)
	}

	return &s
}

func newImageWorkerPool(nworkers int) *ImageWorkerPool {
	s := ImageWorkerPool{tries: make(map[string]int), waiting: make(map[string]chan struct{}), working: make(map[string]bool), workerInput: make(chan *ImageQueryInfo)}

	return &s
}

func (s *ImageDBSession) Query(id string) *ImageQueryFuture {
	recv := make(chan *ImageQueryFuture)
	defer close(recv)

	s.pool.jobs.Add(1)

	go func() { s.pool.workerInput <- &ImageQueryInfo{ID: id, Receiver: recv} }()

	return <-recv
}

func (s *ImageDBSession) Worker(worker_id int) {
	defer s.pool.workers.Done()
	defer Debug.Printf("ImageDBSession worker[%d] finished", worker_id)

	Debug.Printf("ImageDBSession worker[%d] started", worker_id)

	for job := range s.pool.workerInput {
		func() {
			defer s.pool.jobs.Done()

			getFuture := func() (*ImageQueryFuture, bool) {
				s.mutex.Lock()
				defer s.mutex.Unlock()

				if _, ok := s.db[job.ID]; ok {
					done := make(chan struct{})
					close(done)
					Debug.Printf("ImageDBSession worker[%d] creating future for %s, ALREADY EXISTED", worker_id, job.ID)
					return &ImageQueryFuture{ID: job.ID, Done: done, Session: s}, true
				}

				if wp := s.pool.working[job.ID]; wp {
					Debug.Printf("ImageDBSession worker[%d] creating future for %s, PENDING PROCESSING", worker_id, job.ID)

					return &ImageQueryFuture{ID: job.ID, Done: s.pool.waiting[job.ID], Session: s}, true
				} else { // there are no workers working on job.ID

					if _, ok := s.pool.waiting[job.ID]; ok { // Previous worker failed on this job.ID
						Debug.Printf("ImageDBSession worker[%d] creating future for %s, RECOVERING FAILURE", worker_id, job.ID)

						return &ImageQueryFuture{ID: job.ID, Done: s.pool.waiting[job.ID], Session: s}, false
					} else {
						done := make(chan struct{})
						s.pool.waiting[job.ID] = done
						Debug.Printf("ImageDBSession worker[%d] creating future for %s, NOT FOUND", worker_id, job.ID)

						return &ImageQueryFuture{ID: job.ID, Done: done, Session: s}, false
					}
				}
			}

			future, done := getFuture()
			job.Receiver <- future
			if done {
				return
			}

			s.mutex.Lock()
			s.pool.working[job.ID] = true
			s.pool.tries[job.ID]++
			s.mutex.Unlock()

			if img, err := loadImageFromFile(job.ID); err == nil {
				Debug.Printf("ImageDBSession worker[%d]: Querying Image[%s] from cache...\n", worker_id, job.ID)
				func() {
					s.mutex.Lock()
					defer s.mutex.Unlock()
					defer func() { s.pool.working[job.ID] = false }()

					s.db[job.ID] = img
					Debug.Printf("ImageDBSession worker[%d]: Closing Done for %s\n", worker_id, job.ID)
					close(s.pool.waiting[job.ID])
				}()
				Debug.Printf("ImageDBSession worker[%d]: Querying Image[%s] from cache...DONE\n", worker_id, job.ID)
				return
			}
			Debug.Printf("ImageDBSession Worker[%d]: Querying Image[%s] to Triton...\n", worker_id, job.ID)
			img, err := s.client.Get(context.Background(), &compute.GetImageInput{ImageID: job.ID})

			if job.ID == "08aad3e2-672d-11e7-acc4-07e9ace8a210" {
				err = fmt.Errorf("DEBUG")
			}

			if err != nil {
				s.mutex.Lock()
				tries := s.pool.tries[job.ID]
				s.pool.working[job.ID] = false
				Debug.Printf("ImageDBSession Worker[%d]: failed querying %s, %d times", worker_id, job.ID, tries)

				if tries < ImageQueryMaxTries {
					Debug.Printf("ImageDBSession Worker[%d]: re Querying... %s", worker_id, job.ID)
					/*
						s.mutex.Lock()
						delete(s.pool.waiting, job.ID)
						s.mutex.Unlock()
					*/
					//s.mutex.Unlock()
					go func(id string) { s.Query(id) }(job.ID)
					//s.mutex.Lock()
					Debug.Printf("ImageDBSession Worker[%d]: re Querying... done", worker_id)
				} else {
					Debug.Printf("ImageDBSession Worker[%d]: tried querying %d times for %s", worker_id, tries, job.ID)
					img := &compute.Image{}
					img.Tags = make(map[string]string)
					img.Tags["default_user"] = Config.DefaultUser

					s.db[job.ID] = img
					Debug.Printf("ImageDBSession Worker[%d]: closing Done channel of %s\n", worker_id, job.ID)
					close(s.pool.waiting[job.ID])
				}
				s.mutex.Unlock()

			} else {
				s.mutex.Lock()
				s.db[job.ID] = img
				s.pool.working[job.ID] = false

				Debug.Printf("ImageDBSession Worker[%d]: closing Done channel of %s\n", worker_id, job.ID)
				close(s.pool.waiting[job.ID])
				s.mutex.Unlock()
				saveImageToFile(job.ID, img)
			}
		}()
	}
}

func imageinfo_pathname(id string) string {
	return filepath.Join(TsshRoot, "cache", TritonProfileName, "image", id)
}

func saveImageToFile(id string, info *compute.Image) error {
	file := imageinfo_pathname(id)

	os.MkdirAll(filepath.Dir(file), 0755)

	f, err := os.Create(file)
	if err != nil {
		fmt.Printf("cannot open file(%s): %s\n", file, err)
		return fmt.Errorf("cannot open file(%s): %s", file, err)
	}
	defer f.Close()

	b, _ := json.Marshal(*info)
	err = binary.Write(f, binary.LittleEndian, b)

	if err != nil {
		fmt.Printf("cannot write ImageInfo(%s) to the file cache: %s\n", file, err)
		return fmt.Errorf("cannot write ImageInfo(%s) to the file cache: %s", file, err)
	}
	return nil
}

func loadImageFromFile(id string) (*compute.Image, error) {
	file := imageinfo_pathname(id)

	_, err := os.Stat(file)
	if err != nil {
		return nil, fmt.Errorf("no cached found for %s", id)
	}

	b, err := ioutil.ReadFile(file)
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("cannot read cached image information: %s", file)
	}

	var info compute.Image
	err = json.Unmarshal(b, &info)
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("binary.Read(%s) failed: %s", file, err)
	}
	// TODO: need to check INFO whether it's genuine; IOW, remove it if it is empty (zero).
	//       at least it should have id, and one or more networks.
	return &info, nil
}

func DefaultUser(image *compute.Image) string {
	if user, ok := image.Tags["default_user"]; ok {
		return user
	} else {
		return "root"
	}
}

func images_main() {
	keyId := os.Getenv("SDC_KEY_ID")
	accountName := os.Getenv("SDC_ACCOUNT")
	keyPath := os.Getenv("SDC_KEY_FILE")
	url := os.Getenv("SDC_URL")
	signer, err := GetSigner(accountName, keyId, keyPath)
	if err != nil {
		fmt.Printf("error: %s\n", err)
		os.Exit(1)
	}

	config := triton.ClientConfig{TritonURL: url, AccountName: accountName, Signers: []authentication.Signer{signer}}

	cClient, err := compute.NewClient(&config)
	session := NewImageSession(cClient.Images(), ImageQueryMaxWorkers)

	scanner := bufio.NewScanner(os.Stdin)

	seen := map[string]bool{}
	for scanner.Scan() {
		fmt.Printf("read: %s\n", scanner.Text())
		f := session.Query(scanner.Text())
		fmt.Printf("Future: %s\n", f)
		seen[scanner.Text()] = true
	}

	for k, _ := range seen {
		resp := session.Query(k)
		fmt.Printf("Future: %s\n", resp)
		fmt.Printf("calling Get() for %s\n", k)
		img, _ := resp.Get()
		//fmt.Printf("%s[%d] = %s\n", k, session.pool.tries[k], DefaultUser(img))
		fmt.Printf("%s = %s\n", k, DefaultUser(img))
	}

	fmt.Printf("Wait...\n")
	session.pool.jobs.Wait()
	/*
		for k, v := range ImgSession.tries {
			if info, ok := ImgSession.db[k]; ok {
				fmt.Printf("%s[%d] = %s\n", k, v, DefaultUser(info))
			} else {
				fmt.Printf("%s[%d] = *FAILED*\n", k, v)
			}
		}
	*/
}
