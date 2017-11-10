package main

import (
	"sync"
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

func NewCacheSession(updater CacheUpdateFunc, retries int, cacheOnError bool, nworkers int) *CacheSession {
	s := CacheSession{
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

	for i := 0; i < nworkers; i++ {
		go s.cacheSessionWorker(i)
	}

	go s.server()

	return &s
}

func (s *CacheSession) Close() {
	Debug.Printf("CacheSession.Close(): closing READ channel")
	close(s.readChan)
	close(s.peekChan)
}

func (s *CacheSession) finalize() {
	Debug.Printf("CacheSession.finalize(): closing JOB QUEUE channel")
	close(s.jobQueue)
	Debug.Printf("CacheSession.finalize(): readChan CLOSED!")

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
	defer Debug.Printf("CacheSession.server(): end")

	Debug.Printf("CacheSession.server(): start")
	serial := 0
	for {
		select {
		case req := <-s.peekChan:
			Debug.Printf("CacheSession.server[%d]: PEEK: Request(key = %s) received", serial, req.key)
			if req == (PeekRequest{}) {
				break
			}

			_, ok := s.db[req.key]
			go func(in chan bool, val bool) {
				in <- val
			}(req.receiver, ok)

		case req := <-s.readChan:
			Debug.Printf("CacheSession.server[%d]: READ: Request(key = %s) received", serial, req.key)
			if req == (ReadRequest{}) {
				s.finalize()
				return
			}

			img, ok := s.db[req.key]
			if ok {
				if req.receiver != nil {
					Debug.Printf("CacheSession.server[%d]: READ: craete goroutine for returning value for key = %s", serial, req.key)
					go func(in chan interface{}, img interface{}) {
						defer close(in)
						in <- img
					}(req.receiver, img)
				}
			} else { // s.db[req.key] is not ready.
				Debug.Printf("CacheSession.server[%d]: READ: no entry in the DB for key = %s", serial, req.key)

				if _, ok := s.waitingClients[req.key]; ok {
					if req.receiver != nil {
						s.waitingClients[req.key] = append(s.waitingClients[req.key], req.receiver)
						Debug.Printf("CacheSession.server[%d]: READ: added Request(key = %s) to the waiting list (%d waiters)", serial, req.key, len(s.waitingClients[req.key]))
						Debug.Printf("CacheSession.server[%d]: READ: waiting list: %v", serial, s.waitingClients[req.key])
					} else {
						Debug.Printf("CacheSession.server[%d]: READ: Request(key = %s) has no receiver", serial, req.key)
					}

				} else {
					Debug.Printf("CacheSession.server[%d]: READ: none in waiting list, send Request(key = %s) to job queue", serial, req.key)
					s.waitingClients[req.key] = append(s.waitingClients[req.key], req.receiver)
					go func(r ReadRequest) {
						s.jobQueue <- r
					}(req)
				}

			}
			Debug.Printf("CacheSession.server[%d]: READ: done Request(key = %s)", serial, req.key)

		case req := <-s.writeChan:
			if req == (WriteRequest{}) {
				break
			}

			Debug.Printf("CacheSession.server[%d]: WRITE: Request(key = %s) received", serial, req.key)

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
				Debug.Printf("CacheSession.server[%d]: WRITE: broadcasted value of DB[%s] to all waiters", serial, req.key)
			}

			Debug.Printf("CacheSession.server[%d]: WRITE: done Request(key = %s)", serial, req.key)
		}
		serial++
	}
}

func (s *CacheSession) cacheSessionWorker(workerId int) {
	s.workers.Add(1)
	defer s.workers.Done()

	serial := 0
	for req := range s.jobQueue {
		Debug.Printf("CacheSession.worker[%d:%d]: retrieved Request(key = %s)", workerId, serial, req.key)

		Debug.Printf("CacheSession.worker[%d:%d]: calling Updater(key = %s)", workerId, serial, req.key)

		var value interface{}
		var err error
		for i := 0; i < s.Retries+1; i++ {
			value, err = s.Updater(req.key)
			if err == nil {
				break
			}
			Debug.Printf("CacheSession.worker[%d:%d]: Updater(key = %s) failed for %d times, err = %s", workerId, serial, req.key, i+1, err)
		}

		// s.writeChan <- WriteRequest{key: req.key, value: &img}
		if err == nil {
			/*
				go func(ch chan WriteRequest, req WriteRequest) {
					ch <- req
				}(s.writeChan, WriteRequest{key: req.key, value: value})
			*/
			Debug.Printf("CacheSession.worker[%d:%d]: Updater(%s) success; sending WriteRequest() to the server", workerId, serial, req.key)
			s.writeChan <- WriteRequest{key: req.key, value: value}
		} else {
			Debug.Printf("CacheSession.worker[%d:%d]: Updater(%s) failure; err = %s", workerId, serial, req.key, err)

			Debug.Printf("CacheSession.worker[%d:%d]: Caching err for key = %s", workerId, serial, err)
			s.writeChan <- WriteRequest{key: req.key, value: err}
		}

		/*
			if req.receiver != nil {
				if err == nil {
					Debug.Printf("CacheSession.worker[%d:%d]: sending the value for ReadRequest(%s) to the requester", workerId, serial, req.key)
					go func(in chan interface{}, v interface{}) {
						defer close(in)
						in <- v
					}(req.receiver, value)
				} else {
					Debug.Printf("CacheSession.worker[%d:%d]: sending an error for ReadRequest(%s) to the requester", workerId, serial, req.key)
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
	defer func() { Debug.Printf("Prepare() done") }()

	go func() { s.readChan <- ReadRequest{key: key} }()
}

func (s *CacheSession) Get(key string) (interface{}, error) {
	defer func() { Debug.Printf("Get() done") }()
	ch := make(chan interface{})
	s.readChan <- ReadRequest{key: key, receiver: ch}

	value := <-ch

	if err, ok := value.(error); ok {
		return nil, err
	}
	return value, nil
}

func (s *CacheSession) Peek(key string) bool {
	defer func() { Debug.Printf("Peek() done") }()
	ch := make(chan bool)
	s.peekChan <- PeekRequest{key: key, receiver: ch}

	return <-ch
}
