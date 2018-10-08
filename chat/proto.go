package chat

import (
	"context"
	"io"
	"log"

	"github.com/ethereum/go-ethereum/p2p"
	"github.com/oklog/run"
)

type Peer struct {
	Peer *p2p.PeerInfo
	OutC chan<- string
}

type RxMsg struct {
	Peer *p2p.PeerInfo
	Msg  string
}

func newChatProtocol(peerC chan<- Peer, inC chan<- RxMsg) p2p.Protocol {
	return p2p.Protocol{
		Name:    "chat",
		Version: 1,
		Length:  1,
		Run:     genChatRun(peerC, inC),
	}
}

func chatOut(ctx context.Context, outC <-chan string, w p2p.MsgWriter) error {
theLoop:
	for {
		select {
		case data := <-outC:
			if err := p2p.Send(w, 0, data); err != nil {
				return err
			}
		case <-ctx.Done():
			break theLoop
		}
	}
	return nil
}

func chatIn(ctx context.Context, inC chan<- RxMsg, peer *p2p.Peer, r p2p.MsgReader) error {
theLoop:
	for {
		msg, err := r.ReadMsg()
		if err != nil {
			if err == io.EOF {
				log.Printf("info: %s: peer disconnected", peer.RemoteAddr().String())
			} else {
				log.Printf("error: %s: message read failed: %s", peer.RemoteAddr().String(), err)
			}
			return err
		}

		var msgData string
		if err = msg.Decode(&msgData); err != nil {
			log.Printf("error: %s: message decode failed: %s", peer.RemoteAddr().String(), err)
			continue
		}

		select {
		case inC <- RxMsg{Peer: peer.Info(), Msg: msgData}:
		case <-ctx.Done():
			break theLoop
		}
	}
	return nil
}

func genChatRun(peerC chan<- Peer, inC chan<- RxMsg) func(peer *p2p.Peer, ws p2p.MsgReadWriter) error {
	return func(peer *p2p.Peer, ws p2p.MsgReadWriter) error {
		outC := make(chan string)
		defer close(outC)

		log.Printf("info: %s: peer connected", peer.RemoteAddr().String())
		peerC <- Peer{peer.Info(), outC}

		ctx := context.Background()
		g := run.Group{}
		{
			ctx, cancel := context.WithCancel(ctx)
			g.Add(func() error {
				return chatOut(ctx, outC, ws)
			}, func(error) {
				cancel()
			})
		}
		{
			ctx, cancel := context.WithCancel(ctx)
			g.Add(func() error {
				return chatIn(ctx, inC, peer, ws)
			}, func(error) {
				cancel()
			})
		}

		err := g.Run()
		peerC <- Peer{peer.Info(), nil}
		return err
	}
}
