// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/jbdamiano/go-gtp/gtpv1"
	"github.com/jbdamiano/go-gtp/gtpv1/ie"
	"github.com/jbdamiano/go-gtp/gtpv1/message"
	"github.com/jbdamiano/go-gtp/gtpv2"
)

func (s *sgw) handleCreateSessionResponse(s5cConn gtpv1.Conn, pgwAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), pgwAddr)
	if s.mc != nil {
		s.mc.messagesReceived.WithLabelValues(pgwAddr.String(), msg.MessageTypeName()).Inc()
	}

	s5Session, err := s5cConn.GetSessionByTEID(msg.TEID(), pgwAddr)
	if err != nil {
		return err
	}

	// assert type to refer to the struct field specific to the message.
	// in general, no need to check if it can be type-asserted, as long as the MessageType is
	// specified correctly in AddHandler().
	csRspFromPGW := msg.(*message.CreatePDPContextResponse)

	// check Cause value first.
	if causeIE := csRspFromPGW.Cause; causeIE != nil {
		cause, err := causeIE.Cause()
		if err != nil {
			return err
		}
		if cause != gtpv1.ResCauseRequestAccepted {
			s5cConn.RemoveSession(s5Session)
			// this is not such a fatal error worth stopping the whole program.
			// in the real case it is better to take some action based on the Cause, though.
			return &gtpv2.CauseNotOKError{
				MsgType: csRspFromPGW.MessageTypeName(),
				Cause:   cause,
				Msg:     fmt.Sprintf("subscriber: %s", s5Session.IMSI),
			}
		}
	} else {
		s5cConn.RemoveSession(s5Session)
		return &gtpv2.RequiredIEMissingError{
			Type: ie.Cause,
		}
	}

	if fteidcIE := csRspFromPGW.TEIDCPlane; fteidcIE != nil {
		teid, err := fteidcIE.TEID()
		if err != nil {
			return err
		}
		s5Session.AddTEID(gtpv1.S5PGWGTPC, teid)
	} else {
		s5cConn.RemoveSession(s5Session)
		return &gtpv2.RequiredIEMissingError{Type: ie.TEIDCPlane}
	}

	if fteidcIE := csRspFromPGW.TEIDDataI; fteidcIE != nil {
		teid, err := fteidcIE.TEID()
		if err != nil {
			return err
		}
		s5Session.AddTEID(gtpv1.S5PGWGTPU, teid)
	} else {
		s5cConn.RemoveSession(s5Session)
		return &gtpv2.RequiredIEMissingError{Type: ie.TEIDDataI}
	}

	if err := s5Session.Activate(); err != nil {
		s5cConn.RemoveSession(s5Session)
		return err
	}

	s11Session, err := s.s11Conn.GetSessionByIMSI(s5Session.IMSI)
	if err != nil {
		return err
	}

	ip := ""
	if fip := csRspFromPGW.GGSNAddressForUserTraffic; fip != nil {
		ip, err = fip.IPAddress()
		if err != nil {
			return err
		}
	} else {
		s5cConn.RemoveSession(s5Session)
		return &gtpv2.RequiredIEMissingError{Type: ie.TEIDDataI}
	}
	teid, err := s5Session.GetTEID(gtpv1.S5PGWGTPU)
	if err := s.handleFTEIDU(ip, teid, s5Session); err != nil {
		return err
	}

	log.Printf("PAss to s11Session for %s", s5Session.IMSI)
	if err := gtpv1.PassMessageTo(s11Session, csRspFromPGW, 5*time.Second); err != nil {
		return err
	}

	log.Printf("Done")

	return nil
}

func (s *sgw) handleModifyBearerResponse(s5cConn gtpv1.Conn, pgwAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), pgwAddr)
	if s.mc != nil {
		s.mc.messagesReceived.WithLabelValues(pgwAddr.String(), msg.MessageTypeName()).Inc()
	}

	s5Session, err := s5cConn.GetSessionByTEID(msg.TEID(), pgwAddr)
	if err != nil {
		return err
	}

	s11Session, err := s.s11Conn.GetSessionByIMSI(s5Session.IMSI)
	if err != nil {
		return err
	}

	if err := gtpv1.PassMessageTo(s11Session, msg, 5*time.Second); err != nil {
		return err
	}

	// even the cause indicates failure, session should be removed locally.
	log.Printf("Modify bearer for Subscriber: %s", s5Session.IMSI)
	return nil
}

func (s *sgw) handleDeleteSessionResponse(s5cConn gtpv1.Conn, pgwAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), pgwAddr)
	if s.mc != nil {
		s.mc.messagesReceived.WithLabelValues(pgwAddr.String(), msg.MessageTypeName()).Inc()
	}

	s5Session, err := s5cConn.GetSessionByTEID(msg.TEID(), pgwAddr)
	if err != nil {
		return err
	}

	s11Session, err := s.s11Conn.GetSessionByIMSI(s5Session.IMSI)
	if err != nil {
		return err
	}

	if err := gtpv1.PassMessageTo(s11Session, msg, 5*time.Second); err != nil {
		return err
	}

	// even the cause indicates failure, session should be removed locally.
	log.Printf("Session deleted for Subscriber: %s", s5Session.IMSI)
	s5cConn.RemoveSession(s5Session)
	return nil
}
