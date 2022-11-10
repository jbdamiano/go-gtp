#!/bin/bash
ls /go-gtp/


CONFIG_PATH="${CONFIG_PATH:-/go-gtp/config/}"
MTU="${DEV_MTU:-1400}"

if [[ -n "${DEV1}" ]]; then
  ifconfig $DEV1 mtu $MTU
fi

if [[ -n "${DEV2}" ]]; then
  ifconfig $DEV2 mtu $MTU
fi

if [[ $ELEMENT == sgw && $K8S == 1 ]]
then
  ip route add $IP_PFCP via $ITF_IP dev $DEV_PFCP
  ip route add $IP_GTPP via $ITF_IP dev $DEV_GTPP
fi

if [[ $ELEMENT == sgw && $VPP == 1 ]]
then
  apt install iputils-ping ethtool

  ip route add  10.40.1.0/24 via 172.19.0.4
  ethtool --offload  eth0  rx off  tx off
  arp -i eth0 -s 10.40.1.2   02:42:ac:13:00:06
fi
if [[ $ELEMENT == enb && $STOP == 1 ]]
then
  tail -f /dev/null
else
  /go-gtp/$ELEMENT -config "$CONFIG_PATH"/$ELEMENT.yml
fi
