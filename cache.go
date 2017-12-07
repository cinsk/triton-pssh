package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	l "github.com/cinsk/triton-pssh/log"
)

type ReadRequest struct {
	key      string
	receiver chan interface{}
}

type WriteRequest struct {
	key   string
	value interface{}
}

type PeekRequest struct {
	key      string
	receiver chan bool
}

type CacheUpdateFunc func(string) (interface{}, error)

type CacheSession struct {
	Name         string
	Updater      CacheUpdateFunc
	Retries      int
	CacheOnError bool

	db             map[string]interface{}
	waitingClients map[string][]chan interface{} // some requesters are waiting for db entry.

	workers sync.WaitGroup

	peekChan chan PeekRequest
	readChan chan ReadRequest

	writeChan chan WriteRequest
	jobQueue  chan ReadRequest
}

func NewCacheSession(name string, updater CacheUpdateFunc, retries int, cacheOnError bool, nWorkers int) *CacheSession {
	s := CacheSession{
		Name:         name,
		Updater:      updater,
		Retries:      retries,
		CacheOnError: cacheOnError,

		db:             make(map[string]interface{}),
		waitingClients: make(map[string][]chan interface{}),
		readChan:       make(chan ReadRequest),
		peekChan:       make(chan PeekRequest),
		jobQueue:       make(chan ReadRequest),
		writeChan:      make(chan WriteRequest),
	}

	l.Debug("starting CacheSession[%v]: retries=%v, cacheOnError=%v, nworkers=%v", name, retries, cacheOnError, nWorkers)
	for i := 0; i < nWorkers; i++ {
		go s.cacheSessionWorker(i)
	}

	go s.server()

	return &s
}

func (s *CacheSession) Close() {
	l.Debug("CacheSession[%v].Close(): closing READ channel", s.Name)
	close(s.readChan)
	close(s.peekChan)
}

func (s *CacheSession) finalize() {
	l.Debug("CacheSession[%v].finalize(): closing JOB QUEUE channel", s.Name)
	close(s.jobQueue)

	done := make(chan struct{})

	go func() {
		s.workers.Wait()
		close(done)
	}()

	for {
		select {
		case <-done:
			close(s.writeChan)
			return
		case <-s.writeChan:
			// do nothing
		}
	}
}

func (s *CacheSession) server() {
	defer l.Debug("CacheSession[%v].server(): end", s.Name)

	l.Debug("CacheSession[%v].server(): start", s.Name)
	serial := 0
	for {
		select {
		case req := <-s.peekChan:
			l.Trace("CacheSession[%v].server[%d]: PEEK: Request(key = %s) received", s.Name, serial, req.key)
			if req == (PeekRequest{}) {
				break
			}

			_, ok := s.db[req.key]
			go func(in chan bool, val bool) {
				in <- val
			}(req.receiver, ok)

		case req := <-s.readChan:
			l.Trace("CacheSession[%v].server[%d]: READ: Request(key = %s) received", s.Name, serial, req.key)
			if req == (ReadRequest{}) {
				s.finalize()
				return
			}

			img, ok := s.db[req.key]
			if ok {
				if req.receiver != nil {
					l.Trace("CacheSession[%v].server[%d]: READ: craete goroutine for returning value for key = %s", s.Name, serial, req.key)
					go func(in chan interface{}, img interface{}) {
						defer close(in)
						in <- img
					}(req.receiver, img)
				}
			} else { // s.db[req.key] is not ready.
				l.Trace("CacheSession[%v].server[%d]: READ: no entry in the DB for key = %s", s.Name, serial, req.key)

				if _, ok := s.waitingClients[req.key]; ok {
					if req.receiver != nil {
						s.waitingClients[req.key] = append(s.waitingClients[req.key], req.receiver)
						l.Trace("CacheSession[%v].server[%d]: READ: added Request(key = %s) to the waiting list (%d waiters)", s.Name, serial, req.key, len(s.waitingClients[req.key]))
						l.Trace("CacheSession[%v].server[%d]: READ: waiting list: %v", s.Name, serial, s.waitingClients[req.key])
					} else {
						l.Trace("CacheSession[%v].server[%d]: READ: Request(key = %s) has no receiver", s.Name, serial, req.key)
					}

				} else {
					l.Trace("CacheSession[%v].server[%d]: READ: none in waiting list, send Request(key = %s) to job queue", s.Name, serial, req.key)
					s.waitingClients[req.key] = append(s.waitingClients[req.key], req.receiver)
					go func(r ReadRequest) {
						s.jobQueue <- r
					}(req)
				}

			}
			l.Trace("CacheSession[%v].server[%d]: READ: done Request(key = %s)", s.Name, serial, req.key)

		case req := <-s.writeChan:
			if req == (WriteRequest{}) {
				break
			}

			l.Trace("CacheSession[%v].server[%d]: WRITE: Request(key = %s) received", s.Name, serial, req.key)

			if _, ok := req.value.(error); ok {
				if s.CacheOnError {
					s.db[req.key] = req.value
				}
			} else {
				s.db[req.key] = req.value
			}

			for _, waitCh := range s.waitingClients[req.key] {
				func(ch chan interface{}, value interface{}) {
					if ch != nil {
						defer close(ch)
						ch <- value
					}
				}(waitCh, req.value)
				l.Trace("CacheSession[%v].server[%d]: WRITE: broadcasted value of DB[%s] to all waiters", s.Name, serial, req.key)
			}

			l.Trace("CacheSession[%v].server[%d]: WRITE: done Request(key = %s)", s.Name, serial, req.key)
		}
		serial++
	}
}

func (s *CacheSession) cacheSessionWorker(workerId int) {
	s.workers.Add(1)
	defer s.workers.Done()

	serial := 0
	for req := range s.jobQueue {
		l.Trace("CacheSession[%v].worker[%d:%d]: retrieved Request(key = %s)", s.Name, workerId, serial, req.key)

		l.Trace("CacheSession[%v].worker[%d:%d]: calling Updater(key = %s)", s.Name, workerId, serial, req.key)

		var value interface{}
		var err error
		for i := 0; i < s.Retries+1; i++ {
			value, err = s.Updater(req.key)
			if err == nil {
				break
			}
			l.Debug("CacheSession[%v].worker[%d:%d]: Updater(key = %s) failed for %d times, error: %s", s.Name, workerId, serial, req.key, i+1, err)
		}

		// s.writeChan <- WriteRequest{key: req.key, value: &img}
		if err == nil {
			/*
				go func(ch chan WriteRequest, req WriteRequest) {
					ch <- req
				}(s.writeChan, WriteRequest{key: req.key, value: value})
			*/
			l.Trace("CacheSession[%v].worker[%d:%d]: Updater(%s) success; sending WriteRequest() to the server", s.Name, workerId, serial, req.key)
			s.writeChan <- WriteRequest{key: req.key, value: value}
		} else {
			l.Debug("CacheSession[%v].worker[%d:%d]: Updater(%s) failure; err = %s", s.Name, workerId, serial, req.key, err)

			l.Debug("CacheSession[%v].worker[%d:%d]: Caching err for key = %s", s.Name, workerId, serial, err)
			s.writeChan <- WriteRequest{key: req.key, value: err}
		}

		/*
			if req.receiver != nil {
				if err == nil {
					l.Debug("CacheSession.worker[%d:%d]: sending the value for ReadRequest(%s) to the requester", workerId, serial, req.key)
					go func(in chan interface{}, v interface{}) {
						defer close(in)
						in <- v
					}(req.receiver, value)
				} else {
					l.Debug("CacheSession.worker[%d:%d]: sending an error for ReadRequest(%s) to the requester", workerId, serial, req.key)
					go func(in chan interface{}, v interface{}) {
						defer close(in)
						in <- v
					}(req.receiver, err)
				}
			}
		*/
		serial++
	}
}

func (s *CacheSession) Prepare(key string) {
	// defer func() { l.Debug("Prepare() done") }()

	go func() { s.readChan <- ReadRequest{key: key} }()
}

func (s *CacheSession) Get(key string) (interface{}, error) {
	// defer func() { l.Debug("Get() done") }()
	ch := make(chan interface{})
	s.readChan <- ReadRequest{key: key, receiver: ch}

	value := <-ch

	if err, ok := value.(error); ok {
		return nil, err
	}
	return value, nil
}

func (s *CacheSession) Peek(key string) bool {
	// defer func() { l.Debug("Peek() done") }()
	ch := make(chan bool)
	s.peekChan <- PeekRequest{key: key, receiver: ch}

	return <-ch
}

func ReadDataFromFileCache(fileName string, expiration time.Duration) ([]byte, error) {
	stat, err := os.Stat(fileName)
	if err != nil {
		return nil, err
	}

	if stat.ModTime().Add(expiration).Before(time.Now()) {
		return nil, fmt.Errorf("cache expired")
	}
	b, err := ioutil.ReadFile(fileName)
	if err != nil {
		remove_err := os.Remove(fileName)
		if remove_err != nil {
			return nil, fmt.Errorf("cannot read %s (%s), nor can delete it (%s)", err, remove_err)
		}
		return nil, fmt.Errorf("cannot read %s (%s), removed")
	}
	return b, nil
}

func WriteDataToFileCache(fileName string, data []byte) error {
	os.MkdirAll(filepath.Dir(fileName), 0755)

	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer f.Close()

	err = binary.Write(f, binary.LittleEndian, data)
	if err != nil {
		return err
	}
	return nil
}

func ReadJsonFromFileCache(fileName string, expiration time.Duration, v interface{}) error {
	b, err := ReadDataFromFileCache(fileName, expiration)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, v)
	if err != nil {
		os.Remove(fileName)
		return err
	}

	return nil
}

func WriteJsonToFileCache(fileName string, v interface{}) error {
	// func WriteDataToFileCache(fileName string, data []byte) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteDataToFileCache(fileName, b)
}
