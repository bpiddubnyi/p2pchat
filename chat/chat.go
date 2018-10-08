package chat

import (
	"context"
	"crypto/ecdsa"
	"log"
	"sync"

	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/discover"
)

type Chat struct {
	srv       p2p.Server
	peerC     chan Peer
	inC       chan RxMsg
	outC      chan string
	peers     map[string]Peer
	initPeers []string

	OnMsg     func(p *p2p.PeerInfo, msg string)
	OnPeerIn  func(p *p2p.PeerInfo)
	OnPeerOut func(p *p2p.PeerInfo)
}

func New(key *ecdsa.PrivateKey, name, listen string, peers []string) (*Chat, error) {
	peerC := make(chan Peer)
	inC := make(chan RxMsg)
	outC := make(chan string)

	return &Chat{
		srv: p2p.Server{Config: p2p.Config{
			NoDiscovery: true,
			MaxPeers:    len(peers),
			PrivateKey:  key,
			Name:        name,
			ListenAddr:  listen,
			Protocols:   []p2p.Protocol{newChatProtocol(peerC, inC)}},
		},
		peerC:     peerC,
		inC:       inC,
		outC:      outC,
		peers:     map[string]Peer{},
		initPeers: peers,
	}, nil
}

func (c *Chat) Run(ctx context.Context) error {
	err := c.srv.Start()
	if err != nil {
		return err
	}

	c.connect()
	c.run(ctx)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		c.finalize()
		wg.Done()
	}()
	c.srv.Stop()
	wg.Wait()
	return nil
}

func (c *Chat) send(ctx context.Context, msg string) {
	for _, p := range c.peers {
		select {
		case <-ctx.Done():
			return
		case p.OutC <- msg:
		}
	}
}

func (c *Chat) connect() {
	for _, p := range c.initPeers {
		n, err := discover.ParseNode(p)
		if err != nil {
			log.Printf("error: failed to parse peer url: %s", err)
			continue
		}
		c.srv.AddPeer(n)
	}
	c.initPeers = nil // help the gc a little bit
}

func (c *Chat) handlePeerAction(p Peer) {
	if _, ok := c.peers[p.Peer.ID]; ok {
		delete(c.peers, p.Peer.ID)
		if c.OnPeerOut != nil {
			c.OnPeerOut(p.Peer)
		}
	} else {
		c.peers[p.Peer.ID] = p
		if c.OnPeerIn != nil {
			c.OnPeerIn(p.Peer)
		}
	}
}

func (c *Chat) run(ctx context.Context) {
	for {
		select {
		case p := <-c.peerC:
			c.handlePeerAction(p)
		case in := <-c.inC:
			if c.OnMsg != nil {
				c.OnMsg(in.Peer, in.Msg)
			}
		case msg := <-c.outC:
			c.send(ctx, msg)
		case <-ctx.Done():
			return
		}
	}
}

func (c *Chat) finalize() {
	for len(c.peers) > 0 {
		// read all the peers finishing sends
		p := <-c.peerC
		c.handlePeerAction(p)
	}
	close(c.peerC)
	close(c.inC)
}

func (c *Chat) Close() {
	close(c.outC)
}

func (c *Chat) Send(ctx context.Context, msg string) {
	select {
	case <-ctx.Done():
	case c.outC <- msg:
	}
}
