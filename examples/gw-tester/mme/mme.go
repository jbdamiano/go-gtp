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
	"github.com/jbdamiano/go-gtp/gtpv2"
	"github.com/jbdamiano/go-gtp/gtpv2/ie"
	"github.com/jbdamiano/go-gtp/gtpv2/message"
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
	session *gtpv2.Session
}

type mme struct {
	s1mmeListener net.Listener
	s11Addr       net.Addr
	s11IP         string
	s11Conn       *gtpv2.Conn

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

	fteid bool

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
	m.s11Addr, err = net.ResolveUDPAddr("udp", cfg.LocalAddrs.S11IP+gtpv2.GTPCPort)
	if err != nil {
		return nil, err
	}
	m.s11IP = cfg.LocalAddrs.S11IP
	m.teid = make(map[string]Sub)

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

	m.s11Conn = gtpv2.NewConn(m.s11Addr, gtpv2.IFTypeS11MMEGTPC, 0)
	go func() {
		if err := m.s11Conn.ListenAndServe(ctx); err != nil {
			log.Println(err)
			return
		}
	}()
	log.Printf("Started serving S11 on: %s", m.s11Addr)

	m.s11Conn.AddHandlers(map[uint8]gtpv2.HandlerFunc{
		message.MsgTypeCreateSessionResponse: m.handleCreateSessionResponse,
		message.MsgTypeModifyBearerResponse:  m.handleModifyBearerResponse,
		message.MsgTypeDeleteSessionResponse: m.handleDeleteSessionResponse,
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

		s1teid, err := session.GetTEID(gtpv2.IFTypeS1USGWGTPU)
		if err != nil {
			errCh <- err
			return
		}

		var subscriber Sub
		subscriber.teid = s1teid
		subscriber.session = session

		m.teid[req.Imsi] = subscriber

		rspCh <- &s1mme.AttachResponse{
			Cause:   s1mme.Cause_SUCCESS,
			SgwAddr: m.sgw.s1uIP + gtpv2.GTPUPort,
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
		log.Printf("Sent Detach Session Request for %s", teid)

		select {
		case <-m.deleted:
			// go forward
		case <-time.After(5 * time.Second):
			errCh <- fmt.Errorf("timed out: %s", teid)
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

func (m *mme) DeleteSession(teid uint32, sess *Session, session *gtpv2.Session) (uint32, error) {

	nteid, err := session.GetTEID(gtpv2.IFTypeS11S4SGWGTPC)
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

func (m *mme) CreateSession(sess *Session) (*gtpv2.Session, error) {
	br := gtpv2.NewBearer(5, "", &gtpv2.QoSProfile{
		PL: 2, QCI: 255, MBRUL: 0xffffffff, MBRDL: 0xffffffff, GBRUL: 0xffffffff, GBRDL: 0xffffffff,
	})
	var pci, pvi uint8
	if br.PCI {
		pci = 1
	}
	if br.PVI {
		pvi = 1
	}

	raddr, err := net.ResolveUDPAddr("udp", m.sgw.s11IP+gtpv2.GTPCPort)
	if err != nil {
		return nil, err
	}

	session, _, err := m.s11Conn.CreateSession(
		raddr,
		ie.NewIMSI(sess.IMSI),
		ie.NewMSISDN(sess.MSISDN),
		ie.NewMobileEquipmentIdentity(sess.IMEISV),
		ie.NewUserLocationInformationStruct(
			ie.NewCGI(m.enb.mcc, m.enb.mnc, 0x1111, 0x2222),
			ie.NewSAI(m.enb.mcc, m.enb.mnc, 0x1111, 0x3333),
			ie.NewRAI(m.enb.mcc, m.enb.mnc, 0x1111, 0x4444),
			ie.NewTAI(m.enb.mcc, m.enb.mnc, 0x5555),
			ie.NewECGI(m.enb.mcc, m.enb.mnc, 0x66666666),
			ie.NewLAI(m.enb.mcc, m.enb.mnc, 0x1111),
			ie.NewMENBI(m.enb.mcc, m.enb.mnc, 0x11111111),
			ie.NewEMENBI(m.enb.mcc, m.enb.mnc, 0x22222222),
		),
		ie.NewRATType(gtpv2.RATTypeEUTRAN),
		ie.NewIndicationFromOctets(0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00),
		m.s11Conn.NewSenderFTEID(m.s11IP, ""),
		ie.NewFullyQualifiedTEID(gtpv2.IFTypeS5S8PGWGTPC, 0, m.pgw.s5cIP, "").WithInstance(1),
		ie.NewAccessPointName(m.apn),
		ie.NewSelectionMode(gtpv2.SelectionModeMSorNetworkProvidedAPNSubscribedVerified),
		ie.NewPDNType(gtpv2.PDNTypeIPv4),
		ie.NewPDNAddressAllocation(sess.SrcIP),
		ie.NewAPNRestriction(gtpv2.APNRestrictionNoExistingContextsorRestriction),
		ie.NewAggregateMaximumBitRate(0, 0),
		ie.NewBearerContext(
			ie.NewEPSBearerID(br.EBI),
			ie.NewBearerQoS(pci, br.PL, pvi, br.QCI, br.MBRUL, br.MBRDL, br.GBRUL, br.GBRDL),
		),
		ie.NewFullyQualifiedCSID(m.s11IP, 1),
		ie.NewServingNetwork(m.mcc, m.mnc),
		ie.NewUETimeZone(9*time.Hour, 0),
	)

	if err != nil {
		return nil, err
	}
	if m.mc != nil {
		m.mc.messagesSent.WithLabelValues(raddr.String(), "Create Session Request").Inc()
	}

	return session, nil
}

func (m *mme) ModifyBearer(sess *gtpv2.Session, sub *Session) (*gtpv2.Bearer, error) {
	teid, err := sess.GetTEID(gtpv2.IFTypeS11S4SGWGTPC)
	if err != nil {
		return nil, err
	}

	fteid := ie.NewFullyQualifiedTEID(gtpv2.IFTypeS1UeNodeBGTPU, sub.itei, m.enb.s1uIP, "")
	if _, err = m.s11Conn.ModifyBearer(
		teid, sess, ie.NewIndicationFromOctets(0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00),
		ie.NewBearerContext(ie.NewEPSBearerID(sess.GetDefaultBearer().EBI), fteid, ie.NewPortNumber(2125)),
	); err != nil {
		return nil, err
	}
	if m.mc != nil {
		m.mc.messagesSent.WithLabelValues(sess.PeerAddr().String(), "Modify Bearer Request").Inc()
	}

	return nil, nil
}
