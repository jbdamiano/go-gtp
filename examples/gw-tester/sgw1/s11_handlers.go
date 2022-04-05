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

func (s *sgw) handleCreateSessionRequest(s11Conn gtpv1.Conn, mmeAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), mmeAddr)
	if s.mc != nil {
		s.mc.messagesReceived.WithLabelValues(mmeAddr.String(), msg.MessageTypeName()).Inc()
	}

	s11Session := gtpv1.NewSession(mmeAddr, &gtpv1.Subscriber{Location: &gtpv1.Location{}})

	// assert type to refer to the struct field specific to the message.
	// in general, no need to check if it can be type-asserted, as long as the MessageType is
	// specified correctly in AddHandler().
	csReqFromMME := msg.(*message.CreatePDPContextRequest)

	var pgwAddrString string

	pgwAddrString = s.pgwAddr + gtpv1.GTPCPort

	log.Printf("route to %s", pgwAddrString)

	teid, err := csReqFromMME.TEIDCPlane.TEID()
	if err != nil {
		return err
	}
	s11Session.AddTEID(gtpv1.S11MMEGTPC, teid)

	raddr, err := net.ResolveUDPAddr("udp", pgwAddrString)
	if err != nil {
		return err
	}

	// keep session information retrieved from the message.
	// XXX - should return error if required IE is missing.
	if imsiIE := csReqFromMME.IMSI; imsiIE != nil {
		imsi, err := imsiIE.IMSI()
		if err != nil {
			return err
		}

		// remove previous session for the same subscriber if exists.
		sess, err := s11Conn.GetSessionByIMSI(imsi)
		if err != nil {
			switch err.(type) {
			case *gtpv1.UnknownIMSIError:
				// whole new session. just ignore.
			default:
				return fmt.Errorf("got something unexpected: %w", err)
			}
		} else {
			s11Conn.RemoveSession(sess)
		}

		s11Session.IMSI = imsi
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.MSISDN}
	}

	if msisdnIE := csReqFromMME.MSISDN; msisdnIE != nil {
		s11Session.MSISDN, err = msisdnIE.MSISDN()
		if err != nil {
			return err
		}
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.MSISDN}
	}

	if meiIE := csReqFromMME.IMEI; meiIE != nil {
		s11Session.IMEI, err = meiIE.IMEISV()
		if err != nil {
			return err
		}
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.IMEISV}
	}

	/* if netIE := csReqFromMME.ServingNetwork; netIE != nil {
		s11Session.MCC, err = netIE.MCC()
		if err != nil {
			return err
		}
		s11Session.MNC, err = netIE.MNC()
		if err != nil {
			return err
		}
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.ServingNetwork}
	}
	*/
	if ratIE := csReqFromMME.RATType; ratIE != nil {
		s11Session.RATType, err = ratIE.RATType()
		if err != nil {
			return err
		}
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.RATType}
	}
	s11sgwFTEID := s11Conn.NewSenderCTEID()

	s11sgwTEID := s11sgwFTEID.MustTEID()
	s11Conn.RegisterSession(s11sgwTEID, s11Session)

	s5cFTEID := s.s5cConn.NewSenderCTEID()

	// Generate a fakse TEIDu
	s5uFTEID := s.s5cConn.FirstUteid()

	var s5Session *gtpv1.Session
	var seq uint16

	if euaIE := csReqFromMME.RATType; euaIE != nil {

		s5Session, seq, err = s.s5cConn.CreateSession(
			raddr,
			csReqFromMME.IMSI, csReqFromMME.MSISDN, csReqFromMME.IMEI, csReqFromMME.UserLocationInformation,
			csReqFromMME.RATType, s5cFTEID, s5uFTEID,
			csReqFromMME.APN, csReqFromMME.SelectionMode,
			csReqFromMME.EndUserAddress,
			ie.NewGSNAddress(s.s5cIP),
			// False GSNu
			ie.NewGSNAddress("1.1.1.1"),
			csReqFromMME.QoSProfile,
		)
	} else {
		s5Session, seq, err = s.s5cConn.CreateSession(
			raddr,
			csReqFromMME.IMSI, csReqFromMME.MSISDN, csReqFromMME.IMEI, csReqFromMME.UserLocationInformation,
			csReqFromMME.RATType, s5cFTEID, s5uFTEID,
			csReqFromMME.APN, csReqFromMME.SelectionMode,
			ie.NewGSNAddress(s.s5cIP),
			// False GSNu
			ie.NewGSNAddress("1.1.1.1"),
		)
	}
	if err != nil {
		return err
	}
	s5Session.AddTEID(gtpv1.S5SGWGTPU, s5uFTEID.MustTEID())

	log.Printf("Sent Create Session Request to %s for %s", pgwAddrString, s5Session.IMSI)
	if s.mc != nil {
		s.mc.messagesSent.WithLabelValues(mmeAddr.String(), "Create Session Request").Inc()
	}

	var csRspFromSGW *message.CreatePDPContextResponse
	s11mmeTEID, err := s11Session.GetTEID(gtpv1.S11MMEGTPC)
	if err != nil {
		s11Conn.RemoveSession(s11Session)
		return err
	}

	incomingMsg, err := s11Session.WaitMessage(seq, 5*time.Second)
	if err != nil {
		csRspFromSGW = message.NewCreatePDPContextResponse(
			s11mmeTEID, 0,
			ie.NewCause(gtpv1.APNRestrictionPrivate2),
		)

		if err := s11Conn.RespondTo(mmeAddr, csReqFromMME, csRspFromSGW); err != nil {
			s11Conn.RemoveSession(s11Session)
			return err
		}
		log.Printf(
			"Sent %s with failure code: %d, target subscriber: %s",
			csRspFromSGW.MessageTypeName(), gtpv2.CausePGWNotResponding, s11Session.IMSI,
		)
		s11Conn.RemoveSession(s11Session)
		return err
	}

	var csRspFromPGW *message.CreatePDPContextResponse
	switch m := incomingMsg.(type) {
	case *message.CreatePDPContextResponse:
		// move forward
		csRspFromPGW = m

	default:
		s11Conn.RemoveSession(s11Session)
		return &gtpv2.RequiredIEMissingError{Type: ie.MSISDN}
		//return &gtpv2.UnexpectedTypeError{Msg: incomingMsg}
	}

	// if everything in CreateSessionResponse seems OK, relay it to MME.
	//s1usgwFTEID := s.s1uConn.NewSenderUTEID()
	s1usgwFTEID := s.s5cConn.NewSenderUTEID()
	csRspFromSGW = csRspFromPGW
	csRspFromSGW.TEIDCPlane = nil
	csRspFromSGW.TEIDCPlane = s11sgwFTEID
	csRspFromSGW.TEIDDataI = s1usgwFTEID
	csRspFromSGW.GGSNAddressForCPlane = ie.NewGSNAddress(s.s11IP)
	csRspFromSGW.GGSNAddressForUserTraffic = ie.NewGSNAddress(s.s1uIP)

	//csRspFromSGW.SGWFQCSID = ie.NewFullyQualifiedCSID(s.s1uIP, 1).WithInstance(1)
	csRspFromSGW.SetTEID(s11mmeTEID)
	csRspFromSGW.SetLength()

	s11Session.AddTEID(gtpv1.S11SGWGTPC, s11sgwTEID)
	s11Session.AddTEID(gtpv1.S11SGWGTPU, s1usgwFTEID.MustTEID())

	if err := s11Conn.RespondTo(mmeAddr, csReqFromMME, csRspFromSGW); err != nil {
		s11Conn.RemoveSession(s11Session)
		return err
	}
	if s.mc != nil {
		s.mc.messagesSent.WithLabelValues(mmeAddr.String(), csRspFromSGW.MessageTypeName()).Inc()
	}

	s5cpgwTEID, err := s5Session.GetTEID(gtpv1.S5PGWGTPC)
	if err != nil {
		s11Conn.RemoveSession(s11Session)
		return err
	}
	s5csgwTEID, err := s5Session.GetTEID(gtpv1.S5SGWGTPC)
	if err != nil {
		s11Conn.RemoveSession(s11Session)
		return err
	}

	if err := s11Session.Activate(); err != nil {
		s11Conn.RemoveSession(s11Session)
		return err
	}

	log.Printf(
		"Session created with MME and P-GW for Subscriber: %s;\n\tS11 MME:  %s, TEID->: %#x, TEID<-: %#x\n\tS5C P-GW: %s, TEID->: %#x, TEID<-: %#x",
		s5Session.Subscriber.IMSI, mmeAddr, s11mmeTEID, s11sgwTEID, pgwAddrString, s5cpgwTEID, s5csgwTEID,
	)
	return nil
}

func (s *sgw) handleModifyBearerRequest(s11Conn gtpv1.Conn, mmeAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), mmeAddr)
	if s.mc != nil {
		s.mc.messagesReceived.WithLabelValues(mmeAddr.String(), msg.MessageTypeName()).Inc()
	}

	s11Session, err := s11Conn.GetSessionByTEID(msg.TEID(), mmeAddr)
	if err != nil {
		return err
	}
	s5cSession, err := s.s5cConn.GetSessionByIMSI(s11Session.IMSI)
	if err != nil {
		return err
	}

	mbReqFromMME := msg.(*message.UpdatePDPContextRequest)
	if brCtxIE := mbReqFromMME.SGSNAddressForCPlane; brCtxIE != nil {

		enbIP, err := brCtxIE.IPAddress()
		if err != nil {
			return err
		}
		if enbteid := mbReqFromMME.TEIDDataI; enbteid != nil {
			if err := s.handleFTEIDU(enbIP, enbteid.MustTEID(), s11Session); err != nil {
				return err
			}
		} else {
			return &gtpv2.RequiredIEMissingError{Type: ie.TEIDDataI}
		}
	} else {
		return &gtpv2.RequiredIEMissingError{Type: ie.SGSNNumber}
	}

	// Regereate a TEIDc a new TEIDu and spefy tge reak GSNu
	s5uFTEID := s.s5cConn.NewSenderUTEID()
	s5cSession.AddTEID(gtpv1.S5SGWGTPU, s5uFTEID.MustTEID())
	tmpTeid, err := s5cSession.GetTEID(gtpv1.S5SGWGTPC)
	s5cFTEID := s.s5cConn.NewReplaceSenderCTEID(tmpTeid)

	s11mmeTEID, err := s11Session.GetTEID(gtpv1.S11MMEGTPC)
	if err != nil {
		log.Printf("S11MMEGTPC not found")
		return err
	}
	s1usgwTEID, err := s11Session.GetTEID(gtpv1.S11SGWGTPU)
	if err != nil {
		log.Printf("S11SGWGTPU not found")
		return err
	}
	s5usgwTEID, err := s5cSession.GetTEID(gtpv1.S5SGWGTPU)
	if err != nil {
		log.Printf("S5SGWGTPU not found")
		return err
	}

	if s.useKernelGTP {
		/* if err := s.s1uConn.AddTunnelOverride(
			net.ParseIP(enbIP), net.ParseIP(s1uBearer.SubscriberIP), s1uBearer.OutgoingTEID(), s1usgwTEID,
		); err != nil {
			return err
		}
		if err := s.s5uConn.AddTunnelOverride(
			net.ParseIP(pgwIP), net.ParseIP(s5uBearer.SubscriberIP), s5uBearer.OutgoingTEID(), s5usgwTEID,
		); err != nil {
			return err
		} */
	} else {
		if err := s.s1uConn.RelayTo(
			s.s5uConn, s1usgwTEID, s5cSession.OutgoingTEID(), s5cSession.RemoteAddress(),
		); err != nil {
			return err
		}
		if err := s.s5uConn.RelayTo(
			s.s1uConn, s5usgwTEID, s11Session.OutgoingTEID(), s11Session.RemoteAddress(),
		); err != nil {
			return err
		}
	}

	s5cpgwTEID, err := s5cSession.GetTEID(gtpv1.S5PGWGTPC)
	if err != nil {
		return err
	}

	seq, err := s.s5cConn.ModifyBearer(s5cpgwTEID, s5cSession, s5cFTEID, s5uFTEID,
		ie.NewGSNAddress(s.s5cIP),
		ie.NewGSNAddress(s.s5uIP),
	)
	if err != nil {
		return err
	}

	var mbRspFromPGW *message.UpdatePDPContextResponse

	incomingMessage, err := s11Session.WaitMessage(seq, 5*time.Second)
	if err != nil {
		mbRspFromPGW = message.NewUpdatePDPContextResponse(
			s11mmeTEID, 0,
			ie.NewCause(gtpv2.CausePGWNotResponding),
		)

		if err := s11Conn.RespondTo(mmeAddr, mbReqFromMME, mbRspFromPGW); err != nil {
			return err
		}
		log.Printf(
			"Sent %s with failure code: %d, target subscriber: %s",
			mbRspFromPGW.MessageTypeName(), gtpv2.CausePGWNotResponding, s11Session.IMSI,
		)
		if s.mc != nil {
			s.mc.messagesSent.WithLabelValues(mmeAddr.String(), mbReqFromMME.MessageTypeName()).Inc()
		}
		return err
	}

	switch m := incomingMessage.(type) {
	case *message.UpdatePDPContextResponse:
		// move forward
		mbRspFromPGW = m

	default:
		s11Conn.RemoveSession(s11Session)
		return &gtpv1.CauseNotOKError{Msg: "toto"}
	}

	mbRspFromSGW := message.NewUpdatePDPContextResponse(
		s11mmeTEID, 0,
		ie.NewCause(gtpv1.ResCauseRequestAccepted),
	)

	if err := s11Conn.RespondTo(mmeAddr, msg, mbRspFromSGW); err != nil {
		return err
	}
	if s.mc != nil {
		s.mc.messagesSent.WithLabelValues(mmeAddr.String(), mbRspFromSGW.MessageTypeName()).Inc()
	}

	log.Printf(
		"Started listening on U-Plane for Subscriber: %s;\n\tS1-U: %s\n\tS5-U: %s",
		s11Session.IMSI, s.s1uConn.LocalAddr(), s.s5uConn.LocalAddr(),
	)
	return nil
}

func (s *sgw) handleDeleteSessionRequest(s11Conn gtpv1.Conn, mmeAddr net.Addr, msg message.Message) error {
	log.Printf("Received %s from %s", msg.MessageTypeName(), mmeAddr)
	if s.mc != nil {
		s.mc.messagesReceived.WithLabelValues(mmeAddr.String(), msg.MessageTypeName()).Inc()
	}

	// assert type to refer to the struct field specific to the message.
	// in general, no need to check if it can be type-asserted, as long as the MessageType is
	// specified correctly in AddHandler().
	dsReqFromMME := msg.(*message.DeletePDPContextRequest)

	s11Session, err := s11Conn.GetSessionByTEID(msg.TEID(), mmeAddr)
	if err != nil {
		return err
	}

	s5Session, err := s.s5cConn.GetSessionByIMSI(s11Session.IMSI)
	if err != nil {
		return err
	}

	s5cpgwTEID, err := s5Session.GetTEID(gtpv1.S5PGWGTPC)
	if err != nil {
		return err
	}

	seq, err := s.s5cConn.DeleteSession(
		s5cpgwTEID, s5Session,
	)
	if err != nil {
		return err
	}

	var dsRspFromSGW *message.DeletePDPContextResponse
	s11mmeTEID, err := s11Session.GetTEID(gtpv1.S11MMEGTPC)
	if err != nil {
		return err
	}

	incomingMessage, err := s11Session.WaitMessage(seq, 5*time.Second)
	if err != nil {
		dsRspFromSGW = message.NewDeletePDPContextResponse(
			s11mmeTEID, 0,
			ie.NewCause(gtpv1.APNRestrictionPrivate2),
		)

		if err := s11Conn.RespondTo(mmeAddr, dsReqFromMME, dsRspFromSGW); err != nil {
			return err
		}
		log.Printf(
			"Sent %s with failure code: %d, target subscriber: %s",
			dsRspFromSGW.MessageTypeName(), gtpv2.CausePGWNotResponding, s11Session.IMSI,
		)
		if s.mc != nil {
			s.mc.messagesSent.WithLabelValues(mmeAddr.String(), dsRspFromSGW.MessageTypeName()).Inc()
		}
		return err
	}

	// use the cause as it is.
	switch m := incomingMessage.(type) {
	case *message.DeletePDPContextResponse:
		// move forward
		dsRspFromSGW = m
	default:
		return &gtpv1.CauseNotOKError{Msg: "toto"}
	}

	dsRspFromSGW.SetTEID(s11mmeTEID)
	if err := s11Conn.RespondTo(mmeAddr, msg, dsRspFromSGW); err != nil {
		return err
	}

	log.Printf("Session deleted for Subscriber: %s", s11Session.IMSI)
	if s.mc != nil {
		s.mc.messagesSent.WithLabelValues(mmeAddr.String(), dsRspFromSGW.MessageTypeName()).Inc()
	}

	s11Conn.RemoveSession(s11Session)
	return nil
}

func (s *sgw) handleFTEIDU(ip string, teid uint32, session *gtpv1.Session) error {

	addr, err := net.ResolveUDPAddr("udp", ip+gtpv1.GTPUPort)
	if err != nil {
		return err
	}
	session.SetRemoteAddress(addr)
	session.SetOutgoingTEID(teid)

	return nil
}
