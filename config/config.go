package config

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"sync/atomic"

	"ktkr.us/pkg/airlift/shorthash"
	"ktkr.us/pkg/gas/auth"
)

var OnSave func(*Config)

var (
	sharedConfig = new(atomic.Value)
	confPath     string
	Default      Config
	mu           = new(sync.RWMutex)
)

// Init loads the config from disk into memory, creating it if it doesn't exist
// already.
func Init(filePath string) error {
	confPath = filePath
	c, err := Load()
	if err != nil {
		return err
	}
	sharedConfig.Store(c)
	return nil
}

// Config is a global configuration for Airlift.
type Config struct {
	Host              string
	Port              int
	Password          []byte
	Salt              []byte
	Directory         string
	HashLen           int
	Age               int   // max age of uploads in days
	Size              int64 // max total size of uploads in MB
	AppendExt         bool  // append extensions to returned file URLs
	TwitterCardEnable bool  // enable Twitter Card preview for embeddable files
	TwitterHandle     string
}

// Secrets satisfies gas.User interface.
func (c Config) Secrets() (pass, salt []byte, err error) {
	return c.Password, c.Salt, nil
}

// Username satisfies the gas.User interface. It returns nothing, as Airlift
// doesn't use usernames.
func (c Config) Username() string {
	return ""
}

// MaxAge satisfies the cache.Config interface.
func (c Config) MaxAge() int { return c.Age }

// MaxSize satisfies the cache.Config interface.
func (c Config) MaxSize() int64 { return c.Size }

// MaxCount satisfies the cache.Config interface.
func (c Config) MaxCount() int { return 0 }

// Refresh satisfies the cache.Config interface.
func (c *Config) Refresh() {
	cc := Get()
	*c = *cc
}

func (c *Config) ProcessHash(buf []byte) string {
	return shorthash.Make(buf, c.HashLen)
}

// SetPass updates the config with the new password hash, generating a new
// random salt.
func (c *Config) SetPass(pass string) {
	c.Salt = make([]byte, 32)
	rand.Read(c.Salt)
	c.Password = auth.Hash([]byte(pass), c.Salt)
}

func Get() *Config {
	return sharedConfig.Load().(*Config)
}

func Set(c *Config) error {
	sharedConfig.Store(c)
	return Save(c)
}

func Reload() error {
	c, err := Load()
	if err != nil {
		return err
	}
	Set(c)
	return nil
}

func Load() (*Config, error) {
	mu.RLock()
	conf := Default

	confFile, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			mu.RUnlock()
			err = Save(&conf)
			if err != nil {
				return nil, err
			}
		} else {
			mu.RUnlock()
			return nil, fmt.Errorf("reading config: %v", err)
		}
	} else {
		b, err := ioutil.ReadAll(confFile)
		if err != nil {
			mu.RUnlock()
			return nil, fmt.Errorf("reading config: %v", err)
		}
		err = json.Unmarshal(b, &conf)
		if err != nil {
			mu.RUnlock()
			return nil, fmt.Errorf("decoding config: %v", err)
		}
		// save any new defaults in case the config structure changed
		mu.RUnlock()
		err = Save(&conf)
		if err != nil {
			return nil, err
		}
	}

	return &conf, nil
}

func Save(conf *Config) error {
	mu.Lock()
	defer mu.Unlock()
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
