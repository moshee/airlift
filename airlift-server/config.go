package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"ktkr.us/pkg/gas/auth"
)

type Config struct {
	Host      string
	Port      int
	Password  []byte
	Salt      []byte
	Directory string
	MaxAge    int   // max age of uploads in days
	MaxSize   int64 // max total size of uploads in MB
}

func (conf *Config) loadFiles() (*FileList, error) {
	files := new(FileList)
	// make sure the uploads folder is there, and then load all of the file
	// names and IDs into memory
	os.MkdirAll(conf.Directory, os.FileMode(0700))
	files.Files = make(map[string]os.FileInfo)
	list, err := ioutil.ReadDir(conf.Directory)
	if err != nil {
		return nil, err
	}
	for _, file := range list {
		parts := strings.SplitN(file.Name(), ".", 2)
		files.Files[parts[0]] = file
		files.Size += file.Size()
	}

	return files, nil
}

// satisfies gas.User interface
func (c Config) Secrets() (pass, salt []byte, err error) {
	return c.Password, c.Salt, nil
}

func (c Config) Username() string {
	return ""
}

// Update the config with the new password hash, generating a new random salt
func (c *Config) setPass(pass string) {
	c.Salt = make([]byte, 32)
	rand.Read(c.Salt)
	c.Password = auth.Hash([]byte(pass), c.Salt)
}

func configServer() {
	sharedConf, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case conf := <-configChan:
			err = writeConfig(conf)
			errChan <- err
			if err != nil {
				log.Printf("Failed to write config: %v", err)
			} else {
				log.Printf("Config updated on disk.")
				sharedConf = conf
			}
		case configChan <- sharedConf:
		case <-reloadChan:
			conf, err := loadConfig()
			if err != nil {
				log.Printf("Failed to reload config: %v", err)
			} else {
				log.Print("Reloaded config.")
				sharedConf = conf
			}
		}
	}
}

func loadConfig() (*Config, error) {
	if err := os.MkdirAll(appDir, os.FileMode(0700)); err != nil {
		return nil, err
	}
	var conf Config

	confFile, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			conf = defaultConfig
			err = writeConfig(&conf)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("reading config: %v", err)
		}
	} else {
		b, err := ioutil.ReadAll(confFile)
		if err != nil {
			return nil, fmt.Errorf("reading config: %v", err)
		}
		err = json.Unmarshal(b, &conf)
		if err != nil {
			return nil, fmt.Errorf("decoding config: %v", err)
		}
	}

	return &conf, nil
}

func writeConfig(conf *Config) error {
	b, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return fmt.Errorf("encoding config: %v", err)
	}
	file, err := os.OpenFile(confPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0600))
	if err != nil {
		return fmt.Errorf("writing config: %v", err)
	}
	defer file.Close()
	file.Write(b)
	return nil
}
