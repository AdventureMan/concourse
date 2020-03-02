# NOTE: this Dockerfile is purely for local development! it is *not* used for
# the official 'concourse/concourse' image.

FROM concourse/dev


# download go modules separately so this doesn't re-run on every change
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN grep '^replace' go.mod || go mod download


# containerd tooling
ARG RUNC_VERSION=v1.0.0-rc9
ARG CNI_VERSION=v0.8.3
ARG CONTAINERD_VERSION=1.3.2

# make `ctr` target the default concourse namespace
ENV CONTAINERD_NAMESPACE=concourse

RUN set -x && \
	apt install -y curl iptables && \
	curl -sSL https://github.com/containerd/containerd/releases/download/v$CONTAINERD_VERSION/containerd-$CONTAINERD_VERSION.linux-amd64.tar.gz \
		| tar -zvxf - -C /usr/local/concourse/bin --strip-components=1 && \
	curl -sSL https://github.com/opencontainers/runc/releases/download/$RUNC_VERSION/runc.amd64 \ 
		-o /usr/local/concourse/bin/runc && chmod +x /usr/local/concourse/bin/runc && \
	curl -sSL https://github.com/containernetworking/plugins/releases/download/$CNI_VERSION/cni-plugins-linux-amd64-$CNI_VERSION.tgz \
		| tar -zvxf - -C /usr/local/concourse/bin


# build Concourse without using 'packr' and set up a volume so the web assets
# live-update
COPY . .
RUN go build -gcflags=all="-N -l" -o /usr/local/concourse/bin/concourse \
      ./cmd/concourse
VOLUME /src

ARG GOARCH=amd64

# build Fly into fly-assets
RUN rm /usr/local/concourse/fly-assets/*
RUN GOOS=linux go build -gcflags=all="-N -l" -o fly-linux/fly ./fly
RUN cd fly-linux && tar -czf "/usr/local/concourse/fly-assets/fly-linux-$GOARCH.tgz" fly

# build the init executable for containerd
RUN  set -x && \
	gcc -O2 -static -o /usr/local/concourse/bin/init ./cmd/init/init.c


# generate keys (with 1024 bits just so they generate faster)
RUN mkdir -p /concourse-keys
RUN concourse generate-key -t rsa -b 1024 -f /concourse-keys/session_signing_key
RUN concourse generate-key -t ssh -b 1024 -f /concourse-keys/tsa_host_key
RUN concourse generate-key -t ssh -b 1024 -f /concourse-keys/worker_key
RUN cp /concourse-keys/worker_key.pub /concourse-keys/authorized_worker_keys
