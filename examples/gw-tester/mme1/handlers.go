// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"net"

	"github.com/jbdamiano/go-gtp/gtpv1"
	"github.com/jbdamiano/go-gtp/gtpv1/message"
	"github.com/jbdamiano/go-gtp/gtpv2"
	"github.com/jbdamiano/go-gtp/gtpv2/ie"
)

func (m *mme) handleCreatePDPContextResponse(c gtpv1.Conn, sgwAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), sgwAddr)
	if m.mc != nil {
		m.mc.messagesReceived.WithLabelValues(sgwAddr.String(), msg.MessageTypeName()).Inc()
	}

	// find the session associated with TEID
	session, err := c.GetSessionByTEID(msg.TEID(), sgwAddr)
	if err != nil {
		c.RemoveSession(session)
		return err
	}

	// assert type to refer to the struct field specific to the message.
	// in general, no need to check if it can be type-asserted, as long as the MessageType is
	// specified correctly in AddHandler().
	csRspFromSGW := msg.(*message.CreatePDPContextResponse)

	// check Cause value first.
	if causeIE := csRspFromSGW.Cause; causeIE != nil {
		cause, err := causeIE.Cause()
		if err != nil {
			return err
		}
		if cause != gtpv1.ResCauseRequestAccepted {
			c.RemoveSession(session)
			return &gtpv1.CauseNotOKError{
				MsgType: csRspFromSGW.MessageTypeName(),
				Cause:   cause,
				Msg:     fmt.Sprintf("subscriber: %s", session.IMSI),
			}
		}
	} else {
		//return &gtpv1.RequiredIEMissingError{Type: msg.MessageType()}
	}

	if ggsnIE := csRspFromSGW.GGSNAddressForUserTraffic; ggsnIE != nil {
		m.sgw.s1uIP, err = ggsnIE.IPAddress()
		if err != nil {
			return err
		}
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.BearerContext}
	}

	if euaIE := csRspFromSGW.EndUserAddress; euaIE != nil {
		var ip string

		ip, err = euaIE.IPAddress()
		if err != nil {
			return err
		}
		log.Printf("IP is %s", ip)
		session.AddIp(ip)
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.PDNAddressAllocation}
	}

	teid, err := csRspFromSGW.TEIDCPlane.TEID()

	session.AddTEID(gtpv1.S11SGWGTPC, teid)

	s11sgwTEID, err := session.GetTEID(gtpv1.S11MMEGTPC)
	if err != nil {
		c.RemoveSession(session)
		return err
	}
	s11mmeTEID, err := session.GetTEID(gtpv1.S11MMEGTPC)
	if err != nil {
		c.RemoveSession(session)
		return err
	}

	teid, err = csRspFromSGW.TEIDDataI.TEID()

	session.AddTEID(gtpv1.S11SGWGTPU, teid)

	if err := session.Activate(); err != nil {
		c.RemoveSession(session)
		return err
	}

	log.Printf(
		"Session created with S-GW for Subscriber: %s;\n\tS11 S-GW: %s, TEID->: %#x, TEID<-: %#x",
		session.Subscriber.IMSI, sgwAddr, s11sgwTEID, s11mmeTEID,
	)
	m.created <- struct{}{}
	return nil
}

func (m *mme) handleUpdatePDPContextResponse(c gtpv1.Conn, sgwAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), sgwAddr)

	if m.mc != nil {
		m.mc.messagesReceived.WithLabelValues(sgwAddr.String(), msg.MessageTypeName()).Inc()
	}

	// find the session associated with TEID
	session, err := c.GetSessionByTEID(msg.TEID(), sgwAddr)
	if err != nil {
		c.RemoveSession(session)
		return err
	}

	// assert type to refer to the struct field specific to the message.
	// in general, no need to check if it can be type-asserted, as long as the MessageType is
	// specified correctly in AddHandler().
	csRspFromSGW := msg.(*message.UpdatePDPContextResponse)

	// check Cause value first.
	if causeIE := csRspFromSGW.Cause; causeIE != nil {
		cause, err := causeIE.Cause()
		if err != nil {
			return err
		}
		if cause != gtpv1.ResCauseRequestAccepted {
			c.RemoveSession(session)
			return &gtpv1.CauseNotOKError{
				MsgType: csRspFromSGW.MessageTypeName(),
				Cause:   cause,
				Msg:     fmt.Sprintf("subscriber: %s", session.IMSI),
			}
		}
	} else {
		//return &gtpv1.RequiredIEMissingError{Type: msg.MessageType()}
	}

	m.modified <- struct{}{}
	return nil
}

func (m *mme) handleDeletePDPContextResponse(c gtpv1.Conn, sgwAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), sgwAddr)
	if m.mc != nil {
		m.mc.messagesReceived.WithLabelValues(sgwAddr.String(), msg.MessageTypeName()).Inc()
	}

	session, err := c.GetSessionByTEID(msg.TEID(), sgwAddr)
	if err != nil {
		return err
	}

	c.RemoveSession(session)
	log.Printf("Session deleted with S-GW for Subscriber: %s", session.IMSI)
	m.deleted <- struct{}{}
	return nil
}
