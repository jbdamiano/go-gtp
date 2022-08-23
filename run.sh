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
  ip route add $IP_PFCP dev $DEV_PFCP
  ip route add $IP_GTPP dev $DEV_GTPP
fi
if [[ $ELEMENT == enb && $K8S == 1 ]]
then
  tail -f /dev/null
else
  /go-gtp/$ELEMENT -config "$CONFIG_PATH"/$ELEMENT.yml
fi