#!/usr/bin/env bash

# verify-suite-partition.sh asserts that the
# "scylla-operator/conformance/parallel-kind" and
# "scylla-operator/conformance/parallel-non-kind" suites partition the
# "scylla-operator/conformance/parallel" suite: every spec selected by
# "parallel" is selected by exactly one of the two sub-suites, and no spec is
# selected by both.
#
# The check exists to prevent silent drift when any of the three suite filter
# expressions in pkg/cmd/tests/tests.go is edited in isolation.

set -euEo pipefail
shopt -s inherit_errexit

# set_intersection prints lines that appear in BOTH sorted files (i.e. A ∩ B).
set_intersection() {
    LC_ALL=C comm -12 "$1" "$2"
}

# set_difference prints lines that appear in the first sorted file but NOT in
# the second (i.e. A \ B).
set_difference() {
    LC_ALL=C comm -23 "$1" "$2"
}

# set_union prints the sorted, deduplicated union of two sorted files (i.e. A ∪ B).
set_union() {
    LC_ALL=C sort -u "$1" "$2"
}

repo_root="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )"
list_script="${repo_root}/hack/list-suite-test-cases.sh"

tmp_dir="$( mktemp -d )"
trap 'rm -rf "${tmp_dir}"' EXIT

parallel_suite="scylla-operator/conformance/parallel"
kind_parallel_suite="scylla-operator/conformance/parallel-kind"
parallel_non_kind_suite="scylla-operator/conformance/parallel-non-kind"

parallel_list="${tmp_dir}/parallel.list"
kind_parallel_list="${tmp_dir}/kind-parallel.list"
parallel_non_kind_list="${tmp_dir}/parallel-non-kind.list"
union_list="${tmp_dir}/union.list"

echo "Listing test cases for suite ${parallel_suite}..." >&2
"${list_script}" "${parallel_suite}" > "${parallel_list}"

echo "Listing test cases for suite ${kind_parallel_suite}..." >&2
"${list_script}" "${kind_parallel_suite}" > "${kind_parallel_list}"

echo "Listing test cases for suite ${parallel_non_kind_suite}..." >&2
"${list_script}" "${parallel_non_kind_suite}" > "${parallel_non_kind_list}"

set_union "${kind_parallel_list}" "${parallel_non_kind_list}" > "${union_list}"

failed=0

# Disjointness: no spec may be selected by BOTH sub-suites.
both="$( set_intersection "${kind_parallel_list}" "${parallel_non_kind_list}" )"
if [[ -n "${both}" ]]; then
    echo >&2
    echo "FAIL: the following test case(s) are selected by BOTH ${kind_parallel_suite} and ${parallel_non_kind_suite} (the sub-suites must be disjoint):" >&2
    printf '  %s\n' "${both}" >&2
    failed=1
fi

# Coverage: every parallel spec must be selected by at least one sub-suite.
uncovered="$( set_difference "${parallel_list}" "${union_list}" )"
if [[ -n "${uncovered}" ]]; then
    echo >&2
    echo "FAIL: the following test case(s) are selected by ${parallel_suite} but by NEITHER ${kind_parallel_suite} nor ${parallel_non_kind_suite} (coverage gap):" >&2
    printf '  %s\n' "${uncovered}" >&2
    failed=1
fi

# No overflow: a sub-suite must not select anything that "parallel" itself excludes.
kind_overflow="$( set_difference "${kind_parallel_list}" "${parallel_list}" )"
if [[ -n "${kind_overflow}" ]]; then
    echo >&2
    echo "FAIL: the following test case(s) are selected by ${kind_parallel_suite} but NOT by ${parallel_suite} (sub-suite overflows the parent):" >&2
    printf '  %s\n' "${kind_overflow}" >&2
    failed=1
fi

non_kind_overflow="$( set_difference "${parallel_non_kind_list}" "${parallel_list}" )"
if [[ -n "${non_kind_overflow}" ]]; then
    echo >&2
    echo "FAIL: the following test case(s) are selected by ${parallel_non_kind_suite} but NOT by ${parallel_suite} (sub-suite overflows the parent):" >&2
    printf '  %s\n' "${non_kind_overflow}" >&2
    failed=1
fi

if [[ "${failed}" -ne 0 ]]; then
    echo >&2
    echo "Suite partition invariant is broken. See pkg/cmd/tests/tests.go for the suite filter expressions." >&2
    exit 1
fi

echo "Suite partition invariant holds." >&2
