package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bpiddubnyi/p2pchat/chat"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/oklog/run"
)

const (
	defaultListenAddress = ":9876"
	defaultKeyFile       = "key"
	defaultCfgFile       = "cfg.json"
)

var (
	cfgFilename = defaultCfgFile
	keyFilename = defaultKeyFile
	showHelp    = false
)

type config struct {
	Name          string   `json:"name"`
	Peers         []string `json:"peers"`
	ListenAddress string   `json:"listen"`
	KeyFile       string   `json:"key"`
}

func parseConfig(cfgFile string) (*config, error) {
	f, err := os.Open(cfgFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	c := config{
		ListenAddress: defaultListenAddress,
		KeyFile:       defaultKeyFile,
	}
	e := json.NewDecoder(f)
	if err = e.Decode(&c); err != nil {
		return nil, err
	}

	if len(c.Peers) == 0 {
		return nil, errors.New("empty peers list")
	}
	return &c, nil
}

func init() {
	flag.StringVar(&cfgFilename, "c", cfgFilename, "path to config file")
	flag.StringVar(&keyFilename, "k", keyFilename, "path to ECDSA private key in x509 format")
	flag.BoolVar(&showHelp, "h", showHelp, "show this help message and exit")
}

func handleInput(ctx context.Context, c *chat.Chat) {
	s := bufio.NewScanner(os.Stdin)
	for ctx.Err() != nil && s.Scan() {
		c.Send(ctx, s.Text())
	}
}

func main() {
	flag.Parse()
	if showHelp {
		flag.Usage()
		return
	}

	cfg, err := parseConfig(cfgFilename)
	if err != nil {
		log.Fatalf("error: failed to parse config: %s", err)
	}

	if len(cfg.KeyFile) == 0 {
		cfg.KeyFile = keyFilename
	}
	_, err = os.Stat(cfg.KeyFile)

	var key *ecdsa.PrivateKey
	if os.IsNotExist(err) {
		log.Println("info: generating new private key")
		key, err = crypto.GenerateKey()
		if err != nil {
			log.Fatalf("error: failed to generate key: %s", err)
		}

		log.Printf("info: saving private key to '%s'", cfg.KeyFile)
		if err = crypto.SaveECDSA(cfg.KeyFile, key); err != nil {
			log.Fatalf("error: failed to save private key to '%s': %s", cfg.KeyFile, err)
		}
	} else if err != nil {
		log.Fatalf("error: key file error: %s", err)
	} else {
		log.Printf("info: reading private key from '%s'", cfg.KeyFile)
		key, err = crypto.LoadECDSA(cfg.KeyFile)
		if err != nil {
			log.Fatalf("error: failed to read private key from file: %s", err)
		}
	}
	log.Printf("info: public key: %x", crypto.FromECDSAPub(&key.PublicKey))

	c, err := chat.New(key, cfg.Name, cfg.ListenAddress, cfg.Peers)
	if err != nil {
		log.Fatalf("error: failed to create chat instanse: %s", err)
	}
	defer c.Close()

	c.OnMsg = func(p *p2p.PeerInfo, msg string) {
		log.Printf("%s: %s", p.Name, msg)
	}
	c.OnPeerIn = func(p *p2p.PeerInfo) {
		log.Printf("%s logged in", p.Name)
	}
	c.OnPeerOut = func(p *p2p.PeerInfo) {
		log.Printf("%s logged out", p.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigC
		log.Printf("info: %s signal received. shutdown gracefully", sig)
		cancel()
	}()

	g := run.Group{}
	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return c.Run(ctx)
		}, func(error) {
			cancel()
		})
	}
	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			handleInput(ctx, c)
			return nil
		}, func(error) {
			cancel()
		})
	}

	if err = g.Run(); err != nil {
		log.Fatalf("error: chat failed: %s", err)
	}
}
