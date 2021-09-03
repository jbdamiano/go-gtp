#!/bin/sh

tar cvfz gogtp.tar.gz examples/ gtpv* utils/ *.go go.*
docker build --no-cache -t gogtp .