package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/joyent/triton-go/compute"
)

func instances_pathname(input *compute.ListInstancesInput) string {
	return filepath.Join(TsshRoot, "cache", TritonProfileName, "instances", fmt.Sprintf("%04d-%06d", input.Limit, input.Offset))
}

func loadInstancesFromFile(input *compute.ListInstancesInput) ([]*compute.Instance, error) {
	file := instances_pathname(input)

	_, err := os.Stat(file)
	if err != nil {
		return nil, fmt.Errorf("no cached found for %d-%d", input.Limit, input.Offset)
	}

	b, err := ioutil.ReadFile(file)
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("cannot read cached image information: %s", file)
	}

	var instances []*compute.Instance
	err = json.Unmarshal(b, &instances)
	if err != nil {
		os.Remove(file)
		return nil, fmt.Errorf("binary.Read(%s) failed: %s", file, err)
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

func ListInstances(client *compute.ComputeClient, context context.Context) chan *compute.Instance {
	var limit uint16 = MAX_LIMIT
	var offset uint16 = 0

	ch := make(chan *compute.Instance, 1)

	go func() {
		defer close(ch)
		var wg sync.WaitGroup

		for {
			Debug.Printf("ListMachine: offset: %v, limit: %v", offset, limit)

			input := &compute.ListInstancesInput{Offset: offset, Limit: limit}
			instances, err := loadInstancesFromFile(input)
			if err != nil {
				if instances, err = client.Instances().List(context, input); err != nil {
					Err(1, err, "ListMachine API failed")
				}
				err = saveInstancesToFile(input, instances)
			} else {
				Debug.Printf("using cached instances offset: %v, limit: %v", offset, limit)
			}

			for _, inst := range instances {
				ImageSession.Query(inst.Image)
				for _, netid := range inst.Networks {
					NetworkSession.Query(netid)
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
