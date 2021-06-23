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

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vishvananda/netlink"

	"strconv"

	"github.com/jbdamiano/go-gtp/gtpv1"
	"github.com/jbdamiano/go-gtp/gtpv2"
	"github.com/jbdamiano/go-gtp/gtpv2/message"
)

type list struct {
	ip       string
	prevList *list
	nextList *list
	last     *list
}

type pgw struct {
	cConn *gtpv2.Conn
	uConn *gtpv1.UPlaneConn

	s5c, s5u string
	sgiIF    string

	useKernelGTP bool

	routeSubnet *net.IPNet
	addedRoutes []*netlink.Route
	addedRules  []*netlink.Rule

	promAddr string
	mc       *metricsCollector
	dynamic  bool

	inactive *list
	active   *list

	imsis map[string]*list

	errCh chan error
}

// push adds another number to the stack
func add_inactive(p *pgw, addr string) {
	var current list
	current.ip = addr
	current.nextList = nil
	current.last = nil
	if p.inactive == nil {
		p.inactive = &current
		current.prevList = nil
	} else {
		current.prevList = p.inactive.last
		p.inactive.last.nextList = &current
	}
	p.inactive.last = &current

}

func rem_inactive(p *pgw) *list {
	current := p.inactive
	p.inactive = current.nextList
	p.inactive.prevList = nil
	p.inactive.last = current.last
	current.last = nil
	current.nextList = nil
	current.prevList = nil
	return current
}

// push adds another number to the stack
func add_active(p *pgw, addr string) {
	var current list
	current.ip = addr
	current.nextList = nil
	current.last = nil
	if p.active == nil {
		p.active = &current
		current.prevList = nil
	} else {
		current.prevList = p.active.last
		p.active.last.nextList = &current
	}
	p.active.last = &current
}

func rem_active(p *pgw) *list {
	current := p.inactive
	p.active = current.nextList
	p.active.prevList = nil
	p.active.last = current.last
	current.last = nil
	current.nextList = nil
	current.prevList = nil
	return current
}

func newPGW(cfg *Config) (*pgw, error) {
	p := &pgw{
		s5c:          cfg.LocalAddrs.S5CIP + gtpv2.GTPCPort,
		s5u:          cfg.LocalAddrs.S5UIP + gtpv2.GTPUPort,
		useKernelGTP: cfg.UseKernelGTP,
		sgiIF:        cfg.SGiIFName,
		dynamic:      cfg.Dynamic,
		inactive:     nil,
		active:       nil,

		errCh: make(chan error, 1),
	}
	p.imsis = make(map[string]*list)

	var err error

	_, p.routeSubnet, err = net.ParseCIDR(cfg.RouteSubnet)
	if err != nil {
		return nil, err
	}
	if cfg.Dynamic {
		var i int
		for i = 0; i < len(cfg.RouteSubnet); i++ {
			if cfg.RouteSubnet[i] == '/' {
				break
			}
		}

		mask, err := strconv.Atoi(cfg.RouteSubnet[i+1:])
		if err != nil {
			return nil, err
		}

		var nbCtx int
		if len(p.routeSubnet.IP) == 4 {

			if mask > 24 {
				nbCtx = (1 << (32 - mask))
			} else {
				nbCtx = (25 - mask) * 254
			}
			a := p.routeSubnet.IP[0]
			b := p.routeSubnet.IP[1]
			c := p.routeSubnet.IP[2]
			d := p.routeSubnet.IP[3]
			var start byte
			if d == 0 {
				start = d + 1
			} else {
				start = d
			}

			log.Printf("range IPv4 %d.%d.%d.%d start %d nb %d mask %d", a, b, c, d, start, nbCtx, mask)
			last := start

			for i := 0; i < nbCtx; i++ {
				addr := fmt.Sprintf("%d.%d.%d.%d", a, b, c, last)
				last++
				add_inactive(p, addr)
				if last == 255 {
					last = 1
					c++

					if c == 255 {
						break
					}
				}
			}
		} else {
			if mask < 112 {
				mask = 112
			}
			nbCtx = (1 << (128 - mask))
			ipv6 := p.routeSubnet.IP
			for i := 0; i < nbCtx; i++ {
				addr := ipv6.String()
				add_inactive(p, addr)
				ipv6[15] += 1
				if ipv6[15] == 0 {
					ipv6[14]++

					if ipv6[14] == 0 {
						break
					}
				}
			}
		}
	}

	if cfg.PromAddr != "" {
		// validate if the address is valid or not.
		if _, err = net.ResolveTCPAddr("tcp", cfg.PromAddr); err != nil {
			return nil, err
		}
		p.promAddr = cfg.PromAddr
	}

	if !p.useKernelGTP {
		log.Println("WARN: U-Plane does not work without GTP kernel module")
	}

	return p, nil
}

func (p *pgw) run(ctx context.Context) error {
	cAddr, err := net.ResolveUDPAddr("udp", p.s5c)
	if err != nil {
		return err
	}
	p.cConn = gtpv2.NewConn(cAddr, gtpv2.IFTypeS5S8PGWGTPC, 0)
	go func() {
		if err := p.cConn.ListenAndServe(ctx); err != nil {
			log.Println(err)
			return
		}
	}()
	log.Printf("Started serving S5-C on %s", cAddr)

	// register handlers for ALL the message you expect remote endpoint to send.
	p.cConn.AddHandlers(map[uint8]gtpv2.HandlerFunc{
		message.MsgTypeCreateSessionRequest: p.handleCreateSessionRequest,
		message.MsgTypeDeleteSessionRequest: p.handleDeleteSessionRequest,
	})

	uAddr, err := net.ResolveUDPAddr("udp", p.s5u)
	if err != nil {
		return err
	}
	p.uConn = gtpv1.NewUPlaneConn(uAddr)
	if p.useKernelGTP {
		if err := p.uConn.EnableKernelGTP("gtp-pgw", gtpv1.RoleGGSN); err != nil {
			return err
		}
	}
	go func() {
		if err = p.uConn.ListenAndServe(ctx); err != nil {
			log.Println(err)
			return
		}
		log.Println("uConn.ListenAndServe exitted")
	}()
	log.Printf("Started serving S5-U on %s", uAddr)

	// start serving Prometheus, if address is given
	if p.promAddr != "" {
		if err := p.runMetricsCollector(); err != nil {
			return err
		}

		http.Handle("/metrics", promhttp.Handler())
		go func() {
			if err := http.ListenAndServe(p.promAddr, nil); err != nil {
				log.Println(err)
			}
		}()
		log.Printf("Started serving Prometheus on %s", p.promAddr)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-p.errCh:
			log.Printf("Warning: %s", err)
		}
	}
}

func (p *pgw) close() error {
	var errs []error
	for _, r := range p.addedRoutes {
		if err := netlink.RouteDel(r); err != nil {
			errs = append(errs, err)
		}
	}
	for _, r := range p.addedRules {
		if err := netlink.RuleDel(r); err != nil {
			errs = append(errs, err)
		}
	}

	if p.uConn != nil {
		if err := p.uConn.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if err := p.cConn.Close(); err != nil {
		errs = append(errs, err)
	}

	close(p.errCh)

	if len(errs) > 0 {
		return fmt.Errorf("errors while closing S-GW: %+v", errs)
	}
	return nil
}

func (p *pgw) setupUPlane(peerIP, msIP net.IP, otei, itei uint32) error {
	if err := p.uConn.AddTunnelOverride(peerIP, msIP, otei, itei); err != nil {
		return err
	}

	ms32 := &net.IPNet{IP: msIP, Mask: net.CIDRMask(32, 32)}
	dlroute := &netlink.Route{ // ip route replace
		Dst:       ms32,                                 // UE's IP
		LinkIndex: p.uConn.KernelGTP.Link.Attrs().Index, // dev gtp-pgw
		Scope:     netlink.SCOPE_LINK,                   // scope link
		Protocol:  4,                                    // proto static
		Priority:  1,                                    // metric 1
		Table:     3001,                                 // table 3001
	}
	if err := netlink.RouteReplace(dlroute); err != nil {
		return err
	}
	p.addedRoutes = append(p.addedRoutes, dlroute)

	link, err := netlink.LinkByName(p.sgiIF)
	if err != nil {
		return err
	}

	ulroute := &netlink.Route{ // ip route replace
		Dst:       p.routeSubnet,      // dst network via SGi
		LinkIndex: link.Attrs().Index, // SGi I/F name
		Scope:     netlink.SCOPE_LINK, // scope link
		Protocol:  4,                  // proto static
		Priority:  1,                  // metric 1
	}
	if err := netlink.RouteReplace(ulroute); err != nil {
		return err
	}
	p.addedRoutes = append(p.addedRoutes, ulroute)

	rules, err := netlink.RuleList(0)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if r.IifName == link.Attrs().Name && r.Dst == ms32 {
			return nil
		}
	}

	rule := netlink.NewRule()
	rule.IifName = link.Attrs().Name
	rule.Dst = ms32
	rule.Table = 3001
	if err := netlink.RuleAdd(rule); err != nil {
		return err
	}
	p.addedRules = append(p.addedRules, rule)

	return nil
}
