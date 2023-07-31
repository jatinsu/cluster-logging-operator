#!/bin/bash
oc project openshift-logging
make redeploy
oc apply -f hack/cr-vector.yaml
rm hack/vector.toml
sleep 4
oc extract secrets/collector-config --keys=vector.toml --to=/home/jatinsredhat/Documents/cluster-logging-operator/hack --confirm