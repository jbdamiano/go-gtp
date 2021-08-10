// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package gtpv1

import (
	"net"
	"sync"

	"github.com/jbdamiano/go-gtp/gtpv1/ie"
	"github.com/jbdamiano/go-gtp/gtpv1/message"
)

// HandlerFunc is a handler for specific GTPv1 message.
type HandlerFunc func(c Conn, senderAddr net.Addr, msg message.Message) error

type msgHandlerMap struct {
	syncMap sync.Map
}

func (m *msgHandlerMap) store(msgType uint8, handler HandlerFunc) {
	m.syncMap.Store(msgType, handler)
}

func (m *msgHandlerMap) load(msgType uint8) (HandlerFunc, bool) {
	handler, ok := m.syncMap.Load(msgType)
	if !ok {
		return nil, false
	}

	return handler.(HandlerFunc), true
}

func newMsgHandlerMap(m map[uint8]HandlerFunc) *msgHandlerMap {
	mhm := &msgHandlerMap{syncMap: sync.Map{}}
	for k, v := range m {
		mhm.store(k, v)
	}

	return mhm
}

func newDefaultMsgHandlerMap() *msgHandlerMap {
	return newMsgHandlerMap(
		map[uint8]HandlerFunc{
			message.MsgTypeEchoRequest:     handleEchoRequest,
			message.MsgTypeEchoResponse:    handleEchoResponse,
			message.MsgTypeErrorIndication: handleErrorIndication,
		},
	)
}

func handleEchoRequest(c Conn, senderAddr net.Addr, msg message.Message) error {
	// this should never happen, as the type should have been assured by
	// msgHandlerMap before this function is called.
	if _, ok := msg.(*message.EchoRequest); !ok {
		return ErrUnexpectedType
	}

	// respond with EchoResponse.
	// respond with EchoResponse.
	return c.RespondTo(
		senderAddr, msg, message.NewEchoResponse(0, ie.NewRecovery(c.Restarts())),
	)
}

func handleEchoResponse(c Conn, senderAddr net.Addr, msg message.Message) error {
	// this should never happen, as the type should have been assured by
	// msgHandlerMap before this function is called.
	if _, ok := msg.(*message.EchoResponse); !ok {
		return ErrUnexpectedType
	}

	// do nothing.
	return nil
}

func handleErrorIndication(c Conn, senderAddr net.Addr, msg message.Message) error {
	// this should never happen, as the type should have been assured by
	// msgHandlerMap before this function is called.
	ind, ok := msg.(*message.ErrorIndication)
	if !ok {
		return ErrUnexpectedType
	}

	// just log and return
	logf("Ignored Error Indication: %v", &ErrorIndicatedError{
		TEID: ind.TEIDDataI.MustTEID(),
		Peer: ind.GTPUPeerAddress.MustIPAddress(),
	})
	return nil
}
