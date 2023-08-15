#!/bin/bash
oc project openshift-logging
make undeploy && make clean
make deploy-image && make deploy-catalog && make install
oc apply -f hack/cr-vector.yaml
oc apply -f hack/cr-custom.yaml
rm hack/vector.toml
sleep 4
oc extract secrets/collector-config --keys=vector.toml --to=/home/jatinsredhat/Documents/cluster-logging-operator/hack --confirm