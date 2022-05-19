#!/bin/sh

tar cvfz gogtp.tar.gz examples/ gtpv* utils/ *.go go.*
minikube image build -t gogtp2 .