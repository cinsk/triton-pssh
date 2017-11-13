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
)

type ImageCache struct {
	client     *compute.ImagesClient
	cache      *CacheSession
	expiration time.Duration
}

func NewImageCache(client *compute.ImagesClient, expiration time.Duration) *ImageCache {
	cache := ImageCache{}

	cache.client = client
	cache.cache = NewCacheSession(cache.Updater, 1, true, ImageQueryMaxWorkers)
	cache.expiration = expiration

	return &cache
}

func (s *ImageCache) Get(key string) (*compute.Image, error) {
	value, err := s.cache.Get(key)

	if err != nil {
		return nil, err
	} else {
		img, _ := value.(*compute.Image)
		return img, nil
	}
}

func (s *ImageCache) Close() {
	s.cache.Close()
}

func (s *ImageCache) Prepare(key string) {
	s.cache.Prepare(key)
}

func (s *ImageCache) Updater(key string) (interface{}, error) {
	if img, err := s.loadImageFromFile(key); err == nil {
		return img, nil
	}

	img, err := s.client.Get(context.Background(), &compute.GetImageInput{ImageID: key})
	if err == nil {
		s.saveImageToFile(key, img)
	}
	return img, err
}

func imageinfo_pathname(id string) string {
	return filepath.Join(TsshRoot, "cache", TritonProfileName, "image", id)
}

func (s *ImageCache) saveImageToFile(id string, info *compute.Image) error {
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

func (s *ImageCache) loadImageFromFile(id string) (*compute.Image, error) {
	file := imageinfo_pathname(id)

	if Config.NoCache {
		return nil, fmt.Errorf("Config.NoCache is true")
	}

	var info compute.Image
	if err := ReadJsonFromFileCache(file, s.expiration, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

func DefaultUser(image *compute.Image) string {
	if image == nil {
		return Config.DefaultUser
	}

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

	signers, err := GetSigners(accountName, keyId, keyPath)
	if err != nil {
		fmt.Printf("error: %s\n", err)
		os.Exit(1)
	}

	config := triton.ClientConfig{TritonURL: url, AccountName: accountName, Signers: signers}

	cClient, err := compute.NewClient(&config)
	session := NewImageCache(cClient.Images(), time.Duration(30)*time.Second)

	scanner := bufio.NewScanner(os.Stdin)

	seen := map[string]bool{}
	for scanner.Scan() {
		fmt.Printf("read: %s\n", scanner.Text())
		session.Prepare(scanner.Text())
		seen[scanner.Text()] = true
	}

	for k, _ := range seen {
		fmt.Printf("calling Get() for %s\n", k)
		img, _ := session.Get(k)
		//fmt.Printf("%s[%d] = %s\n", k, session.pool.tries[k], DefaultUser(img))
		fmt.Printf("%s = %s\n", k, DefaultUser(img))
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
