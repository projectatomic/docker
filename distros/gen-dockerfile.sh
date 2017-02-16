#!/bin/bash
set -euo pipefail
go=$(which go 2>/dev/null || true)
if test -n "${go}"; then
    exec go run ./distros/gen_dockerfile.go
else
    tmpdir=$(mktemp -d /var/tmp/tmp.XXXXX)
    cd ${tmpdir}
    cat >Dockerfile <<EOF
FROM fedora
RUN yum -y install golang && yum clean all
EOF
    if !sudo docker build -t golang-fedora-bootstrap -f Dockerfile . >build.log 2>&1; then
        cat build.log
    fi
    cd -
    sudo docker run --privileged -v $(pwd):/srv --rm -ti golang-fedora-bootstrap /bin/sh -c 'cd /srv && go run /srv/distros/gen_dockerfile.go'
fi
