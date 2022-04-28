// Copyright 2019-2021 go-gtp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"

	"github.com/jbdamiano/go-gtp/examples/gw-tester/s1mme"
	"github.com/jbdamiano/go-gtp/gtpv1"
	"github.com/jbdamiano/go-gtp/gtpv1/ie"
	"github.com/jbdamiano/go-gtp/gtpv1/message"
	"github.com/jbdamiano/go-gtp/gtpv2"
)

// Session represents a subscriber.
type Session struct {
	IMSI   string
	MSISDN string
	IMEISV string

	SrcIP string

	itei uint32
}

type Sub struct {
	teid    uint32
	session *gtpv1.Session
}

type mme struct {
	s1mmeListener net.Listener
	s11Addr       net.Addr
	s11IP         string
	s11Conn       *gtpv1.CPlaneConn
	s1cIP         string

	created  chan struct{}
	modified chan struct{}
	deleted  chan struct{}

	apn      string
	mcc, mnc string

	teid map[string]Sub

	enb struct {
		mcc   string
		mnc   string
		tai   uint16
		eci   uint32
		s1uIP string
	}

	sgw struct {
		s11IP string
		s1uIP string
	}

	pgw struct {
		s5cIP string
	}

	promAddr string
	mc       *metricsCollector

	errCh chan error
}

func newMME(cfg *Config) (*mme, error) {
	m := &mme{
		mcc: cfg.MCC,
		mnc: cfg.MNC,
		apn: cfg.APN,

		created:  make(chan struct{}, 1),
		modified: make(chan struct{}, 1),
		deleted:  make(chan struct{}, 1),

		errCh: make(chan error, 1),
	}
	m.sgw.s11IP = cfg.SgwS11
	m.pgw.s5cIP = cfg.PgwS5C

	// setup S11 conn
	var err error
	m.s11Addr, err = net.ResolveUDPAddr("udp", cfg.LocalAddrs.S11IP+gtpv1.GTPCPort)
	if err != nil {
		return nil, err
	}
	m.s11IP = cfg.LocalAddrs.S11IP
	m.teid = make(map[string]Sub)

	m.s1cIP, _, err = net.SplitHostPort(cfg.LocalAddrs.S1CAddr)
	if err != nil {
		return nil, err
	}

	// setup gRPC server
	m.s1mmeListener, err = net.Listen("tcp", cfg.LocalAddrs.S1CAddr)
	if err != nil {
		return nil, err
	}

	if cfg.PromAddr != "" {
		// validate if the address is valid or not.
		if _, err = net.ResolveTCPAddr("tcp", cfg.PromAddr); err != nil {
			return nil, err
		}
		m.promAddr = cfg.PromAddr
	}

	return m, nil
}

func (m *mme) run(ctx context.Context) error {
	fatalCh := make(chan error, 1)

	srv := grpc.NewServer()
	s1mme.RegisterAttacherServer(srv, m)
	go func() {
		if err := srv.Serve(m.s1mmeListener); err != nil {
			fatalCh <- fmt.Errorf("error on serving gRPC: %w", err)
			return
		}
	}()
	log.Printf("Started serving S1-MME on: %s", m.s1mmeListener.Addr())

	m.s11Conn = gtpv1.NewConn(m.s11Addr, 1, 0, 0)
	go func() {
		if err := m.s11Conn.ListenAndServe(ctx); err != nil {
			log.Println(err)
			return
		}
	}()
	log.Printf("Started serving S11 on: %s", m.s11Addr)

	m.s11Conn.AddHandlers(map[uint8]gtpv1.HandlerFunc{
		message.MsgTypeCreatePDPContextResponse: m.handleCreatePDPContextResponse,
		message.MsgTypeUpdatePDPContextResponse: m.handleUpdatePDPContextResponse,
		message.MsgTypeDeletePDPContextResponse: m.handleDeletePDPContextResponse,
	})

	// start serving Prometheus, if address is given
	if m.promAddr != "" {
		if err := m.runMetricsCollector(); err != nil {
			return err
		}

		http.Handle("/metrics", promhttp.Handler())
		go func() {
			if err := http.ListenAndServe(m.promAddr, nil); err != nil {
				log.Println(err)
			}
		}()
		log.Printf("Started serving Prometheus on %s", m.promAddr)
	}

	for {
		select {
		case <-ctx.Done():
			// srv.Serve returns when lis is closed
			if err := m.s1mmeListener.Close(); err != nil {
				return err
			}
			return nil
		case err := <-fatalCh:
			return err
		}
	}
}

func (m *mme) reload(cfg *Config) error {
	// TODO: implement
	return nil
}

// Attach is called by eNB by gRPC.
func (m *mme) Attach(ctx context.Context, req *s1mme.AttachRequest) (*s1mme.AttachResponse, error) {
	sess := &Session{
		IMSI:   req.Imsi,
		MSISDN: req.Msisdn,
		IMEISV: req.Imeisv,
		SrcIP:  req.SrcIp,
		itei:   req.ITei,
	}

	var err error
	m.enb.s1uIP, _, err = net.SplitHostPort(req.S1UAddr)
	if err != nil {
		return nil, err
	}

	errCh := make(chan error, 1)
	rspCh := make(chan *s1mme.AttachResponse)
	go func() {
		m.enb.mcc = req.Location.Mcc
		m.enb.mnc = req.Location.Mnc
		m.enb.tai = uint16(req.Location.Tai)
		m.enb.eci = req.Location.Eci

		session, err := m.CreateSession(sess)
		if err != nil {
			errCh <- err
			return
		}
		log.Printf("Sent Create Session Request for %s", session.IMSI)

		select {
		case <-m.created:
			// go forward
		case <-time.After(5 * time.Second):
			errCh <- fmt.Errorf("timed out: %s", session.IMSI)
		}

		if _, err = m.ModifyBearer(session, sess); err != nil {
			errCh <- err
			return
		}
		log.Printf("Sent Modify Bearer Request for %s", session.IMSI)

		select {
		case <-m.modified:
			// go forward
		case <-time.After(5 * time.Second):
			errCh <- fmt.Errorf("timed out: %s", session.IMSI)
		}

		s1teid, err := session.GetTEID(gtpv1.S11SGWGTPU)
		if err != nil {
			errCh <- err
			return
		}

		var subscriber Sub
		subscriber.teid = s1teid
		subscriber.session = session

		m.teid[req.Imsi] = subscriber

		log.Printf("Sgwaddr is %s%s OTei %d SrcIP %s", m.sgw.s1uIP, gtpv1.GTPUPort, s1teid, session.GetIp())

		rspCh <- &s1mme.AttachResponse{
			Cause:   s1mme.Cause_SUCCESS,
			SgwAddr: m.sgw.s1uIP + gtpv1.GTPUPort,
			OTei:    s1teid,
			SrcIp:   session.GetIp(),
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case rsp := <-rspCh:
		return rsp, nil
	}
}

func (m *mme) ModifyBearer(sess *gtpv1.Session, sub *Session) (*gtpv2.Bearer, error) {
	teid, err := sess.GetTEID(gtpv1.S11SGWGTPC)
	if err != nil {
		return nil, err
	}

	if _, err = m.s11Conn.ModifyBearer(
		teid, sess,
		ie.NewGSNAddress(m.enb.s1uIP),
		ie.NewTEIDDataI(sub.itei),
	); err != nil {
		return nil, err
	}
	if m.mc != nil {
		m.mc.messagesSent.WithLabelValues(sess.PeerAddr().String(), "Modify Bearer Request").Inc()
	}

	return nil, nil
}

// Detach is called by eNB by gRPC.
func (m *mme) Detach(ctx context.Context, req *s1mme.DetachRequest) (*s1mme.DetachResponse, error) {
	sess := &Session{
		IMSI: req.Imsi,
	}

	var teid = m.teid[req.Imsi].teid
	var session = m.teid[req.Imsi].session

	errCh := make(chan error, 1)
	rspCh := make(chan *s1mme.DetachResponse)
	go func() {

		_, err := m.DeleteSession(teid, sess, session)
		if err != nil {
			errCh <- err
			return
		}
		log.Printf("Sent Detach Session Request for %d", teid)

		select {
		case <-m.deleted:
			// go forward
		case <-time.After(5 * time.Second):
			errCh <- fmt.Errorf("timed out: %d", teid)
		}

		rspCh <- &s1mme.DetachResponse{
			Cause: s1mme.Cause_SUCCESS,
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case rsp := <-rspCh:
		return rsp, nil
	}
}

func (m *mme) DeleteSession(teid uint32, sess *Session, session *gtpv1.Session) (uint16, error) {

	nteid, err := session.GetTEID(2)
	if err != nil {
		return 0, err
	}

	oteid, err := m.s11Conn.DeleteSession(
		nteid, session,
	)
	if err != nil {
		return 0, err
	}

	return oteid, nil
}

func (m *mme) CreateSession(sess *Session) (*gtpv1.Session, error) {

	raddr, err := net.ResolveUDPAddr("udp", m.sgw.s11IP+gtpv1.GTPCPort)
	if err != nil {
		return nil, err
	}

	var session *gtpv1.Session

	if sess.SrcIP[0] != '0' {

		session, _, err = m.s11Conn.CreateSession(
			raddr,
			ie.NewIMSI(sess.IMSI),
			ie.NewMSISDN(sess.MSISDN),
			ie.NewIMEISV(sess.IMEISV),
			ie.NewUserLocationInformationWithSAI(m.enb.mcc, m.enb.mnc, 0x1111, 0x3333),
			ie.NewRATType(gtpv1.RatTypeUTRAN),
			ie.NewQoSProfile([]byte{0x03, 0x1b, 0x93, 0x1f, 0x73, 0x96, 0xfe, 0xfe, 0x74, 0x97, 0xff, 0xff}), // XXX - Implement!
			ie.NewEndUserAddressIPv4(sess.SrcIP),
			ie.NewAccessPointName(m.apn),
			ie.NewSelectionMode(gtpv1.SelectionModeMSorNetworkProvidedAPNSubscribedVerified),
			ie.NewProtocolConfigurationOptions(
				0, ie.NewConfigurationProtocolOption(1, []byte{0xde, 0xad, 0xbe, 0xef}),
			),
			ie.NewGSNAddress(m.s1cIP),
			ie.NewGSNAddress(m.s11IP),
			m.s11Conn.NewSenderCTEID(),
			m.s11Conn.NewSenderUTEID(),

			ie.NewAPNRestriction(gtpv2.APNRestrictionNoExistingContextsorRestriction),
	
			ie.NewMSTimeZone(9*time.Hour, 0),
		)
	} else {
		session, _, err = m.s11Conn.CreateSession(
			raddr,
			ie.NewIMSI(sess.IMSI),
			ie.NewMSISDN(sess.MSISDN),
			ie.NewIMEISV(sess.IMEISV),
			ie.NewUserLocationInformationWithSAI(m.enb.mcc, m.enb.mnc, 0x1111, 0x3333),
			ie.NewRATType(gtpv1.RatTypeUTRAN),
			ie.NewQoSProfile([]byte{0x03, 0x1b, 0x93, 0x1f, 0x73, 0x96, 0xfe, 0xfe, 0x74, 0x97, 0xff, 0xff}), // XXX - Implement!
			ie.NewAccessPointName(m.apn),
			ie.NewSelectionMode(gtpv1.SelectionModeMSorNetworkProvidedAPNSubscribedVerified),
			ie.NewProtocolConfigurationOptions(
				0, ie.NewConfigurationProtocolOption(1, []byte{0xde, 0xad, 0xbe, 0xef}),
			),
			ie.NewGSNAddress(m.s1cIP),
			ie.NewGSNAddress(m.s11IP),
			m.s11Conn.NewSenderCTEID(),
			m.s11Conn.NewSenderUTEID(),

			ie.NewAPNRestriction(gtpv2.APNRestrictionNoExistingContextsorRestriction),
			ie.NewMSTimeZone(9*time.Hour, 0),
		)
	}
	if err != nil {
		return nil, err
	}
	if m.mc != nil {
		m.mc.messagesSent.WithLabelValues(raddr.String(), "Create Session Request").Inc()
	}

	return session, nil
}
