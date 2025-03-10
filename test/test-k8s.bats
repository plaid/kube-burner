#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2030,SC2031,SC2164

load helpers.bash

setup_file() {
  cd k8s
  export BATS_TEST_TIMEOUT=600
  export JOB_ITERATIONS=5
  export QPS=2
  export BURST=2
  setup-kind
  setup-prometheus
}

setup() {
  export UUID; UUID=$(uuidgen)
  export TEMP_FOLDER; TEMP_FOLDER=$(mktemp -d)
  export INDEXING_TYPE=""
}

teardown() {
  echo "Last bats run command: ${BATS_RUN_COMMAND} from $(pwd)"
  kubectl delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

teardown_file() {
  destroy-kind
  podman rm -f prometheus
}

@test "kube-burner init: no indexing" {
  run kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  [ "$status" -eq 0 ]
}

@test "kube-burner init: indexing only pod latency metrics" {
  export INDEXING_TYPE=local
  export LATENCY=true
  run kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  [ "$status" -eq 0 ]
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "kube-burner init: indexing, pod latency metrics and alerting" {
  export INDEXING_TYPE=local
  run kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml -a alert-profile.yaml
  [ "$status" -eq 0 ]
  export LATENCY=true
  export ALERTING=true
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "kube-burner init: indexing and metrics-endpoint" {
  export INDEXING_TYPE=local
  export ALERTING=true
  run kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -e metrics-endpoints.yaml
  [ "$status" -eq 0 ]
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "kube-burner index: metrics-endpoint with single prometheus endpoint" {
  run kube-burner index --uuid="${UUID}"  -u http://localhost:9090 -m metrics-profile.yaml
  [ "$status" -eq 0 ]
}

@test "kube-burner index: metrics-endpoint and sending metrics to ES" {
  run kube-burner index --uuid="${UUID}" -e metrics-endpoints.yaml --es-server=https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com:443 --es-index=ripsaw-kube-burner
  [ "$status" -eq 0 ]
}

@test "kube-burner init: crd" {
  kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/network-attachment-definition-client/master/artifacts/networks-crd.yaml
  run kube-burner init -c kube-burner-crd.yml --uuid="${UUID}"
  [ "$status" -eq 0 ]
  run kubectl delete -f objectTemplates/storageclass.yml
  [ "$status" -eq 0 ]
}
