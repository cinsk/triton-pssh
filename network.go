package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	triton "github.com/joyent/triton-go"
	"github.com/joyent/triton-go/compute"
	"github.com/joyent/triton-go/network"
)

type NetworkCache struct {
	client     *network.NetworkClient
	cache      *CacheSession
	expiration time.Duration
}

func NewNetworkCache(client *network.NetworkClient, expiration time.Duration) *NetworkCache {
	cache := NetworkCache{}

	cache.client = client
	cache.cache = NewCacheSession(cache.Updater, 1, true, NetworkQueryMaxWorkers)
	cache.expiration = expiration

	return &cache
}

func (s *NetworkCache) Get(key string) (*network.Network, error) {
	value, err := s.cache.Get(key)

	if err != nil {
		return nil, err
	} else {
		net, _ := value.(*network.Network)
		return net, nil
	}
}

func (s *NetworkCache) Close() {
	s.cache.Close()
}

func (s *NetworkCache) Prepare(key string) {
	s.cache.Prepare(key)
}

func (s *NetworkCache) Updater(key string) (interface{}, error) {
	if net, err := s.loadNetworkFromFile(key); err == nil {
		return net, nil
	}

	net, err := s.client.Get(context.Background(), &network.GetInput{ID: key})
	if err == nil {
		s.saveNetworkToFile(key, net)
	}
	return net, err
}

func networkinfo_pathname(id string) string {
	return filepath.Join(TsshRoot, "cache", TritonProfileName, "network", id)
}

func (s *NetworkCache) saveNetworkToFile(id string, info *network.Network) error {
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

func (s *NetworkCache) loadNetworkFromFile(id string) (*network.Network, error) {
	file := networkinfo_pathname(id)

	if Config.NoCache {
		return nil, fmt.Errorf("Config.NoCache is true")
	}

	var info network.Network
	if err := ReadJsonFromFileCache(file, s.expiration, &info); err != nil {
		return nil, err
	}

	// TODO: need to check INFO whether it's genuine; IOW, remove it if it is empty (zero).
	//       at least it should have id, and one or more networks.

	return &info, nil
}

func (s *NetworkCache) IsPublic(id string) bool {
	net, err := s.Get(id)
	if err != nil {
		return false
	} else {
		return net.Public
	}
}

func (s *NetworkCache) HasPublic(instance *compute.Instance) bool {
	for _, id := range instance.Networks {
		public := s.IsPublic(id)
		if public {
			return true
		}
	}
	return false
}

func (s *NetworkCache) UserFuncIsPublic(args ...interface{}) (interface{}, error) {
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
	signers, err := GetSigners(accountName, keyId, keyPath)
	if err != nil {
		fmt.Printf("error: %s\n", err)
		os.Exit(1)
	}

	config := triton.ClientConfig{TritonURL: url, AccountName: accountName, Signers: signers}

	client, err := network.NewClient(&config)
	session := NewNetworkCache(client, time.Duration(30)*time.Second)

	scanner := bufio.NewScanner(os.Stdin)

	seen := map[string]bool{}
	for scanner.Scan() {
		fmt.Printf("read: %s\n", scanner.Text())
		session.Prepare(scanner.Text())
		seen[scanner.Text()] = true
	}

	for k, _ := range seen {
		fmt.Printf("calling Get() for %s\n", k)
		net, _ := session.Get(k)
		//fmt.Printf("%s[%d] = %s\n", k, session.pool.tries[k], DefaultUser(net))
		fmt.Printf("%s = %v\n", k, net.Public)
	}

	fmt.Printf("Wait...\n")
	session.Close()

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
