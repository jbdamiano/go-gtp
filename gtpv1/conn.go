// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package gtpv1

import (
	"net"

	"github.com/jbdamiano/go-gtp/gtpv1/ie"
	"github.com/jbdamiano/go-gtp/gtpv1/message"
)

// Conn is an abstraction of both GTPv1-C and GTPv1-U Conn.
type Conn interface {
	net.PacketConn
	AddHandler(uint8, HandlerFunc)
	RespondTo(net.Addr, message.Message, message.Message) error
	Restarts() uint8
	GetSessionByTEID(teid uint32, peer net.Addr) (*Session, error)
	GetSessionByIMSI(imsi string) (*Session, error)
	RemoveSession(session *Session)
	NewSenderCTEID() (fteidIE *ie.IE)
	NewSenderUTEID() (fteidIE *ie.IE)
	NewSenderFTEID() (fteidIE *ie.IE)
	RegisterSession(itei uint32, session *Session)
}
