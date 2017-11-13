package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/joyent/triton-go/compute"
)

const MAX_LIMIT = 1000 // max instances that can be queried at a time

func instances_pathname(input *compute.ListInstancesInput) string {
	return filepath.Join(TsshRoot, "cache", TritonProfileName, "instances", fmt.Sprintf("%04d-%06d", input.Limit, input.Offset))
}

func loadInstancesFromFile(input *compute.ListInstancesInput, expiration time.Duration) ([]*compute.Instance, error) {
	file := instances_pathname(input)

	if Config.NoCache {
		return nil, fmt.Errorf("Config.NoCache is true")
	}

	var instances []*compute.Instance
	if err := ReadJsonFromFileCache(file, expiration, &instances); err != nil {
		return instances, err
	}

	// TODO: need to check INFO whether it's genuine; IOW, remove it if it is empty (zero).
	//       at least it should have id, and one or more networks.
	return instances, nil
}

func saveInstancesToFile(input *compute.ListInstancesInput, instances []*compute.Instance) error {
	file := instances_pathname(input)

	os.MkdirAll(filepath.Dir(file), 0755)

	f, err := os.Create(file)
	if err != nil {
		fmt.Printf("cannot open file(%s): %s\n", file, err)
		return fmt.Errorf("cannot open file(%s): %s", file, err)
	}
	defer f.Close()

	b, _ := json.Marshal(instances)
	err = binary.Write(f, binary.LittleEndian, b)

	if err != nil {
		fmt.Printf("cannot write NetworkInfo(%s) to the file cache: %s\n", file, err)
		return fmt.Errorf("cannot write NetworkInfo(%s) to the file cache: %s", file, err)
	}
	return nil

}

func ListInstances(client *compute.ComputeClient, context context.Context, expiration time.Duration) chan *compute.Instance {
	var limit uint16 = MAX_LIMIT
	var offset uint16 = 0

	ch := make(chan *compute.Instance, 1)

	go func() {
		defer close(ch)
		var wg sync.WaitGroup

		for {
			Debug.Printf("ListMachine: offset: %v, limit: %v", offset, limit)

			input := &compute.ListInstancesInput{Offset: offset, Limit: limit}
			instances, err := loadInstancesFromFile(input, expiration)
			if err != nil {
				if instances, err = client.Instances().List(context, input); err != nil {
					Err(1, err, "ListMachine API failed")
				}
				err = saveInstancesToFile(input, instances)
			} else {
				Debug.Printf("using cached instances offset: %v, limit: %v", offset, limit)
			}

			for _, inst := range instances {
				ImgCache.Prepare(inst.Image)
				for _, netid := range inst.Networks {
					NetCache.Prepare(netid)
				}
				wg.Add(1)
				go func(i *compute.Instance) {
					defer wg.Done()
					ch <- i
				}(inst)

			}
			if len(instances) < int(limit) {
				break
			}
			offset += limit
		}
		wg.Wait()
	}()

	return ch
}
