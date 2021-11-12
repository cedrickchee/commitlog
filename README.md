# commitlog

Commitlog is a distributed commit log service.

## Build a Log Package

Logs—which are sometimes also called _write-ahead logs_, _transaction logs_, or
_commit logs_—are at the heart of storage engines, message queues, version
control, and replication and consensus algorithms. As you build distributed
services, you'll face problems that you can solve with logs.

## Prerequisites

Download and install these softwares:
- Go 1.13+
- [Cloudflare's CFSSL](https://github.com/cloudflare/cfssl) v1.6.1+

Install the Cloudflare CFSSL CLIs by running the following commands:

```sh
$ go get github.com/cloudflare/cfssl/cmd/cfssl@v1.6.1
$ go get github.com/cloudflare/cfssl/cmd/cfssljson@v1.6.1
```

The `cfssl` program, which is the canonical command line utility using the CFSSL
packages.

The `cfssljson` program, which takes the JSON output from the `cfssl` and
programs and writes certificates, keys, CSRs, and bundles to disk.

## Set Up

First, initialize our CA and generate certs.

```sh
$ make init
mkdir -p .config/

$ make gencert
# Generating self-signed root CA certificate and private key
cfssl gencert \
		-initca test/ca-csr.json | cfssljson -bare ca
2021/11/03 00:40:00 [INFO] generating a new CA key and certificate from CSR
2021/11/03 00:40:00 [INFO] generate received request
2021/11/03 00:40:00 [INFO] received CSR
2021/11/03 00:40:00 [INFO] generating key: rsa-2048
2021/11/03 00:40:01 [INFO] encoded CSR
2021/11/03 00:40:01 [INFO] signed certificate with serial number 8147356830437551462081232968300531993326047229

# Generating self-signed server certificate and private key
cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=server \
		test/server-csr.json | cfssljson -bare server
2021/11/03 00:40:01 [INFO] generate received request
2021/11/03 00:40:01 [INFO] received CSR
2021/11/03 00:40:01 [INFO] generating key: rsa-2048
2021/11/03 00:40:01 [INFO] encoded CSR
2021/11/03 00:40:01 [INFO] signed certificate with serial number 402261474156200490360083500727362811589620720837

# Generating multiple client certs and private keys
cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=client \
		-cn="root" \
		test/client-csr.json | cfssljson -bare root-client
2021/11/03 00:40:01 [INFO] generate received request
2021/11/03 00:40:01 [INFO] received CSR
2021/11/03 00:40:01 [INFO] generating key: rsa-2048
2021/11/03 00:40:01 [INFO] encoded CSR
2021/11/03 00:40:01 [INFO] signed certificate with serial number 627656603718368551111127300914672850426637790593

cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=client \
		-cn="nobody" \
		test/client-csr.json | cfssljson -bare nobody-client
2021/11/03 00:40:01 [INFO] generate received request
2021/11/03 00:40:01 [INFO] received CSR
2021/11/03 00:40:01 [INFO] generating key: rsa-2048
2021/11/03 00:40:01 [INFO] encoded CSR
2021/11/03 00:40:01 [INFO] signed certificate with serial number 651985188211974854103183240288947068257645013148

mv *.pem *.csr /home/neo/dev/work/repo/github/commitlog/.config
```

## Test

Now, run your tests with `$ make test`. If all is well, your tests pass and
you’ve made a distributed service that can replicate data.

```sh
# Test output
$ make test
cp test/policy.csv "/home/neo/dev/work/repo/github/commitlog/.config/policy.csv"
cp test/model.conf "/home/neo/dev/work/repo/github/commitlog/.config/model.conf"
go test -race ./...
?   	github.com/cedrickchee/commitlog/api/v1	[no test files]
ok  	github.com/cedrickchee/commitlog/internal/agent	11.445s
?   	github.com/cedrickchee/commitlog/internal/auth	[no test files]
?   	github.com/cedrickchee/commitlog/internal/config	[no test files]
ok  	github.com/cedrickchee/commitlog/internal/discovery	(cached)
ok  	github.com/cedrickchee/commitlog/internal/log	(cached)
ok  	github.com/cedrickchee/commitlog/internal/server	0.275s
ok  	github.com/cedrickchee/commitlog/pkg/freeport	(cached)
```

## What Is Raft and How Does It Work?

[Raft](https://raft.github.io/) is a [consensus](https://en.wikipedia.org/wiki/Consensus_(computer_science)) algorithm that is designed to be easy to understand and implement.

Raft breaks consensus into two parts: leader election and log replication.

The following is a short documentation about Raft's leader election and log replication steps.

### Leader Election

A Raft cluster has one leader and the rest of the servers are followers. The
leader maintains power by sending heartbeat requests to its followers. If the
follower times out waiting for a heartbeat request from the leader, then the
follower becomes a candidate and begins an election to decide the next leader.

### Log Replication

The leader accepts client requests, each of which represents some command to run
across the cluster. (In a key-value service for example, you’d have a command to
assign a key’s value.) For each request, the leader appends the command to its
log and then requests its followers to append the command to their logs. After a
majority of followers have replicated the command—when the leader considers the
command committed—the leader executes the command with a finite-state machine
and responds to the client with the result. The leader tracks the highest
committed offset and sends this in the requests to its followers. When a
follower receives a request, it executes all commands up to the highest
committed offset with its finite-state machine. All Raft servers run the same
finite-state machine that defines how to handle each command.

The recommended number of servers in a Raft cluster is three and five (odd number) because Raft will handle `(N–1)/2` failures, where `N` is the size of your cluster.

Test the distributed log:

```sh
$ make testraft
cp test/policy.csv "/home/neo/dev/work/repo/github/commitlog/.config/policy.csv"
cp test/model.conf "/home/neo/dev/work/repo/github/commitlog/.config/model.conf"
go test -v -race ./internal/log/distributed_test.go
=== RUN   TestMultipleNodes
2021-11-12T19:02:26.341+0800 [INFO]  raft: Initial configuration (index=0): []
2021-11-12T19:02:26.342+0800 [INFO]  raft: Node at 127.0.0.1:24337 [Follower] entering Follower state (Leader: "")
2021-11-12T19:02:26.423+0800 [WARN]  raft: Heartbeat timeout from "" reached, starting election
2021-11-12T19:02:26.423+0800 [INFO]  raft: Node at 127.0.0.1:24337 [Candidate] entering Candidate state in term 2
2021-11-12T19:02:26.430+0800 [DEBUG] raft: Votes needed: 1
2021-11-12T19:02:26.430+0800 [DEBUG] raft: Vote granted from 0 in term 2. Tally: 1
2021-11-12T19:02:26.430+0800 [INFO]  raft: Election won. Tally: 1
2021-11-12T19:02:26.430+0800 [INFO]  raft: Node at 127.0.0.1:24337 [Leader] entering Leader state
2021-11-12T19:02:27.358+0800 [INFO]  raft: Initial configuration (index=0): []
2021-11-12T19:02:27.358+0800 [INFO]  raft: Node at 127.0.0.1:24338 [Follower] entering Follower state (Leader: "")
2021-11-12T19:02:27.358+0800 [INFO]  raft: Updating configuration with AddStaging (1, 127.0.0.1:24338) to [{Suffrage:Voter ID:0 Address:127.0.0.1:24337} {Suffrage:Voter ID:1 Address:127.0.0.1:24338}]
2021-11-12T19:02:27.359+0800 [INFO]  raft: Added peer 1, starting replication
2021/11/12 19:02:27 [DEBUG] raft-net: 127.0.0.1:24338 accepted connection from: 127.0.0.1:58832
2021-11-12T19:02:27.365+0800 [WARN]  raft: Failed to get previous log: 3 rpc error: code = Code(404) desc = offset out of range: 3 (last: 0)
2021-11-12T19:02:27.365+0800 [WARN]  raft: AppendEntries to {Voter 1 127.0.0.1:24338} rejected, sending older logs (next: 1)
2021-11-12T19:02:27.367+0800 [INFO]  raft: pipelining replication to peer {Voter 1 127.0.0.1:24338}
2021/11/12 19:02:27 [DEBUG] raft-net: 127.0.0.1:24338 accepted connection from: 127.0.0.1:58834
2021-11-12T19:02:27.371+0800 [INFO]  raft: Initial configuration (index=0): []
2021-11-12T19:02:27.371+0800 [INFO]  raft: Node at 127.0.0.1:24339 [Follower] entering Follower state (Leader: "")
2021-11-12T19:02:27.372+0800 [INFO]  raft: Updating configuration with AddStaging (2, 127.0.0.1:24339) to [{Suffrage:Voter ID:0 Address:127.0.0.1:24337} {Suffrage:Voter ID:1 Address:127.0.0.1:24338} {Suffrage:Voter ID:2 Address:127.0.0.1:24339}]
2021-11-12T19:02:27.372+0800 [INFO]  raft: Added peer 2, starting replication
2021/11/12 19:02:27 [DEBUG] raft-net: 127.0.0.1:24339 accepted connection from: 127.0.0.1:44036
2021-11-12T19:02:27.374+0800 [WARN]  raft: Failed to get previous log: 4 rpc error: code = Code(404) desc = offset out of range: 4 (last: 0)
2021-11-12T19:02:27.375+0800 [WARN]  raft: AppendEntries to {Voter 2 127.0.0.1:24339} rejected, sending older logs (next: 1)
2021-11-12T19:02:27.375+0800 [INFO]  raft: pipelining replication to peer {Voter 2 127.0.0.1:24339}
2021/11/12 19:02:27 [DEBUG] raft-net: 127.0.0.1:24339 accepted connection from: 127.0.0.1:44038
2021-11-12T19:02:27.477+0800 [INFO]  raft: Updating configuration with RemoveServer (1, ) to [{Suffrage:Voter ID:0 Address:127.0.0.1:24337} {Suffrage:Voter ID:2 Address:127.0.0.1:24339}]
2021-11-12T19:02:27.477+0800 [INFO]  raft: Removed peer 1, stopping replication after 7
2021-11-12T19:02:27.478+0800 [INFO]  raft: aborting pipeline replication to peer {Voter 1 127.0.0.1:24338}
2021/11/12 19:02:27 [ERR] raft-net: Failed to flush response: write tcp 127.0.0.1:24338->127.0.0.1:58832: write: broken pipe
--- PASS: TestMultipleNodes (1.25s)
PASS
ok  	command-line-arguments	1.279s
```
