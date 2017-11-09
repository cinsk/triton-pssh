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
	"github.com/joyent/triton-go/network"
)

type NetworkDBSession struct {
	mutex sync.Mutex
	db    map[string]*network.Network

	pool   *NetworkWorkerPool
	client *network.NetworkClient
}

type NetworkQueryInfo struct {
	ID       string
	Receiver chan *NetworkQueryFuture
}

type NetworkWorkerPool struct {
	tries   map[string]int
	working map[string]bool
	waiting map[string]chan struct{}

	workerInput chan *NetworkQueryInfo

	queryInput chan string

	jobs    sync.WaitGroup
	workers sync.WaitGroup
}

type NetworkQueryFuture struct {
	ID      string
	Done    chan struct{}
	Value   *network.Network
	Session *NetworkDBSession
	Error   error
}

func (f *NetworkQueryFuture) String() string {
	return fmt.Sprintf("{ ID: %s, Done: %v }", f.ID, f.Done)
}

func (f *NetworkQueryFuture) Get() (*network.Network, error) {
	select {
	case <-f.Done:
	}
	f.Session.mutex.Lock()
	defer f.Session.mutex.Unlock()
	f.Value = f.Session.db[f.ID]
	return f.Value, nil
}

func NewNetworkSession(client *network.NetworkClient, nworkers int) *NetworkDBSession {
	s := NetworkDBSession{db: make(map[string]*network.Network), client: client,
		pool: newNetworkWorkerPool(nworkers)}

	for i := 0; i < nworkers; i++ {
		s.pool.workers.Add(1)
		go s.Worker(i)
	}

	return &s
}

func newNetworkWorkerPool(nworkers int) *NetworkWorkerPool {
	s := NetworkWorkerPool{tries: make(map[string]int), waiting: make(map[string]chan struct{}), working: make(map[string]bool), workerInput: make(chan *NetworkQueryInfo)}

	return &s
}

func (s *NetworkDBSession) Query(id string) *NetworkQueryFuture {
	recv := make(chan *NetworkQueryFuture)
	defer close(recv)

	s.pool.jobs.Add(1)

	go func() { s.pool.workerInput <- &NetworkQueryInfo{ID: id, Receiver: recv} }()

	return <-recv
}

func (s *NetworkDBSession) Worker(worker_id int) {
	defer s.pool.workers.Done()
	defer Debug.Printf("NetworkDBSession worker[%d] finished", worker_id)

	Debug.Printf("NetworkDBSession worker[%d] started", worker_id)

	for job := range s.pool.workerInput {
		func() {
			defer s.pool.jobs.Done()

			getFuture := func() (*NetworkQueryFuture, bool) {
				s.mutex.Lock()
				defer s.mutex.Unlock()

				if _, ok := s.db[job.ID]; ok {
					done := make(chan struct{})
					close(done)
					Debug.Printf("NetworkDBSession worker[%d] creating future for %s, ALREADY EXISTED", worker_id, job.ID)
					return &NetworkQueryFuture{ID: job.ID, Done: done, Session: s}, true
				}

				if wp := s.pool.working[job.ID]; wp {
					Debug.Printf("NetworkDBSession worker[%d] creating future for %s, PENDING PROCESSING", worker_id, job.ID)

					return &NetworkQueryFuture{ID: job.ID, Done: s.pool.waiting[job.ID], Session: s}, true
				} else { // there are no workers working on job.ID

					if _, ok := s.pool.waiting[job.ID]; ok { // Previous worker failed on this job.ID
						Debug.Printf("NetworkDBSession worker[%d] creating future for %s, RECOVERING FAILURE", worker_id, job.ID)

						return &NetworkQueryFuture{ID: job.ID, Done: s.pool.waiting[job.ID], Session: s}, false
					} else {
						done := make(chan struct{})
						s.pool.waiting[job.ID] = done
						Debug.Printf("NetworkDBSession worker[%d] creating future for %s, NOT FOUND", worker_id, job.ID)

						return &NetworkQueryFuture{ID: job.ID, Done: done, Session: s}, false
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

			if net, err := loadNetworkFromFile(job.ID); err == nil {
				Debug.Printf("NetworkDBSession worker[%d]: Querying Network[%s] from cache...\n", worker_id, job.ID)
				func() {
					s.mutex.Lock()
					defer s.mutex.Unlock()
					defer func() { s.pool.working[job.ID] = false }()

					s.db[job.ID] = net
					Debug.Printf("NetworkDBSession worker[%d]: Closing Done for %s\n", worker_id, job.ID)
					close(s.pool.waiting[job.ID])
				}()
				Debug.Printf("NetworkDBSession worker[%d]: Querying Network[%s] from cache...DONE\n", worker_id, job.ID)
				return
			}
			Debug.Printf("NetworkDBSession Worker[%d]: Querying Network[%s] to Triton...\n", worker_id, job.ID)
			net, err := s.client.Get(context.Background(), &network.GetInput{ID: job.ID})

			if job.ID == "08aad3e2-672d-11e7-acc4-07e9ace8a210" {
				err = fmt.Errorf("DEBUG")
			}

			if err != nil {
				s.mutex.Lock()
				tries := s.pool.tries[job.ID]
				s.pool.working[job.ID] = false
				Debug.Printf("NetworkDBSession Worker[%d]: failed querying %s, %d times", worker_id, job.ID, tries)

				if tries < NetworkQueryMaxTries {
					Debug.Printf("NetworkDBSession Worker[%d]: re Querying... %s", worker_id, job.ID)
					/*
						s.mutex.Lock()
						delete(s.pool.waiting, job.ID)
						s.mutex.Unlock()
					*/
					//s.mutex.Unlock()
					go func(id string) { s.Query(id) }(job.ID)
					//s.mutex.Lock()
					Debug.Printf("NetworkDBSession Worker[%d]: re Querying... done", worker_id)
				} else {
					Debug.Printf("NetworkDBSession Worker[%d]: tried querying %d times for %s", worker_id, tries, job.ID)
					net := &network.Network{}

					s.db[job.ID] = net
					Debug.Printf("NetworkDBSession Worker[%d]: closing Done channel of %s\n", worker_id, job.ID)
					close(s.pool.waiting[job.ID])
				}
				s.mutex.Unlock()

			} else {
				s.mutex.Lock()
				s.db[job.ID] = net
				s.pool.working[job.ID] = false

				Debug.Printf("NetworkDBSession Worker[%d]: closing Done channel of %s\n", worker_id, job.ID)
				close(s.pool.waiting[job.ID])
				s.mutex.Unlock()
				saveNetworkToFile(job.ID, net)
			}
		}()
	}
}

func networkinfo_pathname(id string) string {
	return filepath.Join(TsshRoot, "cache", TritonProfileName, "network", id)
}

func saveNetworkToFile(id string, info *network.Network) error {
	file := networkinfo_pathname(id)

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
		fmt.Printf("cannot write NetworkInfo(%s) to the file cache: %s\n", file, err)
		return fmt.Errorf("cannot write NetworkInfo(%s) to the file cache: %s", file, err)
	}
	return nil
}

func loadNetworkFromFile(id string) (*network.Network, error) {
	file := imageinfo_pathname(id)

	if Config.NoCache {
		return nil, fmt.Errorf("Config.NoCache is true")
	}

	_, err := os.Stat(file)
	if err != nil {
		return nil, fmt.Errorf("no cached found for %s", id)
	}

	// TODO: check cache expiration

	b, err := ioutil.ReadFile(file)
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("cannot read cached image information: %s", file)
	}

	var info network.Network
	err = json.Unmarshal(b, &info)
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("binary.Read(%s) failed: %s", file, err)
	}
	// TODO: need to check INFO whether it's genuine; IOW, remove it if it is empty (zero).
	//       at least it should have id, and one or more networks.
	return &info, nil
}

func (s *NetworkDBSession) IsPublic(id string) bool {
	future := s.Query(id)
	net, err := future.Get()
	if err != nil {
		return false
	} else {
		return net.Public
	}
}

func (s *NetworkDBSession) HasPublic(instance *compute.Instance) bool {
	for _, id := range instance.Networks {
		public := s.IsPublic(id)
		if public {
			return true
		}
	}
	return false
}

func (s *NetworkDBSession) UserFuncIsPublic(args ...interface{}) (interface{}, error) {
	for _, nid := range args {
		if id, ok := nid.(string); ok {
			public := s.IsPublic(id)
			if public {
				return true, nil
			}
		} else {
			return false, fmt.Errorf("invalid argument type; string value required, found %s[%T]", nid, nid)
		}
	}
	return false, nil
}

func network_main() {
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

	client, err := network.NewClient(&config)
	session := NewNetworkSession(client, NetworkQueryMaxWorkers)

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
		net, _ := resp.Get()
		//fmt.Printf("%s[%d] = %s\n", k, session.pool.tries[k], DefaultUser(net))
		fmt.Printf("%s = %v\n", k, net.Public)
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
