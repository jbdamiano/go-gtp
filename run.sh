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

/go-gtp/$ELEMENT -config "$CONFIG_PATH"/$ELEMENT.yml