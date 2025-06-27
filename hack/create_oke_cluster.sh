#!/bin/bash

CLUSTER_NAME="czeslavo-scylla-testing"
COMPARTMENT_OCID="ocid1.compartment.oc1..aaaaaaaa7go6tkc26yzlzmn7sqpgu2z3b2bqbirhqnobxljocaggffgs6qhq"

# Create a VCN (Virtual Cloud Network) for the cluster.
CREATE_VCN_OUTPUT=$(oci network vcn create \
  --compartment-id "${COMPARTMENT_OCID}" \
  --cidr-block 10.0.0.0/16 \
  --display-name "${CLUSTER_NAME}-vcn")

VCN_OCID=$(echo "${CREATE_VCN_OUTPUT}" | jq -r '.data.id')
