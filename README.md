# commitlog

Commitlog is a distributed commit log service.

This project was created to learn and make a distributed service from first
principles -- distributed computing ideas like service discovery, consensus, and
load balancing.

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
you've made a distributed service that can replicate data.

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
across the cluster. (In a key-value service for example, you'd have a command to
assign a key's value.) For each request, the leader appends the command to its
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


Test distributed service end-to-end (uses Raft for consensus and log replication
and uses Serf for service discovery and cluster membership):

```sh
$ make testagent 
cp test/policy.csv "/home/neo/dev/work/repo/github/commitlog/.config/policy.csv"
cp test/model.conf "/home/neo/dev/work/repo/github/commitlog/.config/model.conf"
go test -v -race ./internal/agent/agent_test.go
=== RUN   TestAgent
2021-11-13T16:09:38.974+0800 [INFO]  raft: Initial configuration (index=0): []
2021-11-13T16:09:38.975+0800 [INFO]  raft: Node at [::]:23314 [Follower] entering Follower state (Leader: "")
2021-11-13T16:09:40.028+0800 [WARN]  raft: Heartbeat timeout from "" reached, starting election
2021-11-13T16:09:40.028+0800 [INFO]  raft: Node at [::]:23314 [Candidate] entering Candidate state in term 2
2021-11-13T16:09:40.033+0800 [DEBUG] raft: Votes needed: 1
2021-11-13T16:09:40.033+0800 [DEBUG] raft: Vote granted from 0 in term 2. Tally: 1
2021-11-13T16:09:40.033+0800 [INFO]  raft: Election won. Tally: 1
2021-11-13T16:09:40.033+0800 [INFO]  raft: Node at [::]:23314 [Leader] entering Leader state
2021/11/13 16:09:40 [INFO] serf: EventMemberJoin: 0 127.0.0.1
2021-11-13T16:09:41.004+0800 [INFO]  raft: Initial configuration (index=0): []
2021-11-13T16:09:41.004+0800 [INFO]  raft: Node at [::]:23316 [Follower] entering Follower state (Leader: "")
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 1 127.0.0.1
2021/11/13 16:09:41 [DEBUG] memberlist: Initiating push/pull sync with:  127.0.0.1:23313
2021/11/13 16:09:41 [DEBUG] memberlist: Stream connection from=127.0.0.1:51938
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 1 127.0.0.1
2021-11-13T16:09:41.013+0800 [INFO]  raft: Updating configuration with AddStaging (1, 127.0.0.1:23316) to [{Suffrage:Voter ID:0 Address:[::]:23314} {Suffrage:Voter ID:1 Address:127.0.0.1:23316}]
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 0 127.0.0.1
2021-11-13T16:09:41.013+0800 [INFO]  raft: Added peer 1, starting replication
2021-11-13T16:09:41.013+0800	DEBUG	membership	discovery/membership.go:165	failed to join	{"error": "node is not the leader", "name": "0", "rpc_addr": "127.0.0.1:23314"}
2021/11/13 16:09:41 [DEBUG] raft-net: [::]:23316 accepted connection from: 127.0.0.1:46600
2021-11-13T16:09:41.025+0800 [INFO]  raft: Initial configuration (index=0): []
2021-11-13T16:09:41.025+0800 [INFO]  raft: Node at [::]:23318 [Follower] entering Follower state (Leader: "")
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 2 127.0.0.1
2021/11/13 16:09:41 [DEBUG] memberlist: Initiating push/pull sync with:  127.0.0.1:23313
2021/11/13 16:09:41 [DEBUG] memberlist: Stream connection from=127.0.0.1:51942
2021-11-13T16:09:41.030+0800 [WARN]  raft: Failed to get previous log: 3 rpc error: code = Code(404) desc = offset out of range: 3 (last: 0)
2021-11-13T16:09:41.030+0800 [WARN]  raft: AppendEntries to {Voter 1 127.0.0.1:23316} rejected, sending older logs (next: 1)
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 2 127.0.0.1
2021-11-13T16:09:41.031+0800 [INFO]  raft: Updating configuration with AddStaging (2, 127.0.0.1:23318) to [{Suffrage:Voter ID:0 Address:[::]:23314} {Suffrage:Voter ID:1 Address:127.0.0.1:23316} {Suffrage:Voter ID:2 Address:127.0.0.1:23318}]
2021-11-13T16:09:41.031+0800 [INFO]  raft: Added peer 2, starting replication
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 1 127.0.0.1
2021-11-13T16:09:41.031+0800 [INFO]  raft: pipelining replication to peer {Voter 1 127.0.0.1:23316}
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 0 127.0.0.1
2021-11-13T16:09:41.031+0800	DEBUG	membership	discovery/membership.go:165	failed to join	{"error": "node is not the leader", "name": "1", "rpc_addr": "127.0.0.1:23316"}
2021-11-13T16:09:41.032+0800	DEBUG	membership	discovery/membership.go:165	failed to join	{"error": "node is not the leader", "name": "0", "rpc_addr": "127.0.0.1:23314"}
2021/11/13 16:09:41 [DEBUG] raft-net: [::]:23318 accepted connection from: 127.0.0.1:39724
2021-11-13T16:09:41.045+0800 [WARN]  raft: Failed to get previous log: 4 rpc error: code = Code(404) desc = offset out of range: 4 (last: 0)
2021-11-13T16:09:41.046+0800 [WARN]  raft: AppendEntries to {Voter 2 127.0.0.1:23318} rejected, sending older logs (next: 1)
2021-11-13T16:09:41.046+0800 [INFO]  raft: pipelining replication to peer {Voter 2 127.0.0.1:23318}
2021/11/13 16:09:41 [DEBUG] raft-net: [::]:23316 accepted connection from: 127.0.0.1:46606
2021/11/13 16:09:41 [DEBUG] raft-net: [::]:23318 accepted connection from: 127.0.0.1:39728
2021/11/13 16:09:41 [INFO] serf: EventMemberJoin: 2 127.0.0.1
2021/11/13 16:09:41 [DEBUG] serf: messageJoinType: 1
2021/11/13 16:09:41 [DEBUG] serf: messageJoinType: 1
2021/11/13 16:09:41 [DEBUG] serf: messageJoinType: 2
2021/11/13 16:09:41 [DEBUG] serf: messageJoinType: 1
2021/11/13 16:09:41 [DEBUG] serf: messageJoinType: 2

...

2021-11-13T16:09:44.053+0800	INFO	server	zap/options.go:212	finished unary call with code OK	{"grpc.start_time": "2021-11-13T16:09:44+08:00", "system": "grpc", "span.kind": "server", "grpc.service": "log.v1.Log", "grpc.method": "Produce", "peer.address": "127.0.0.1:42768", "grpc.code": "OK", "grpc.time_ns": 686930}
2021-11-13T16:09:44.055+0800	INFO	server	zap/options.go:212	finished unary call with code OK	{"grpc.start_time": "2021-11-13T16:09:44+08:00", "system": "grpc", "span.kind": "server", "grpc.service": "log.v1.Log", "grpc.method": "Consume", "peer.address": "127.0.0.1:42768", "grpc.code": "OK", "grpc.time_ns": 375171}
2021-11-13T16:09:47.083+0800	INFO	server	zap/options.go:212	finished unary call with code OK	{"grpc.start_time": "2021-11-13T16:09:47+08:00", "system": "grpc", "span.kind": "server", "grpc.service": "log.v1.Log", "grpc.method": "Consume", "peer.address": "127.0.0.1:46612", "grpc.code": "OK", "grpc.time_ns": 388583}
2021-11-13T16:09:47.085+0800	ERROR	server	zap/options.go:212	finished unary call with code Code(404)	{"grpc.start_time": "2021-11-13T16:09:47+08:00", "system": "grpc", "span.kind": "server", "grpc.service": "log.v1.Log", "grpc.method": "Consume", "peer.address": "127.0.0.1:42768", "error": "rpc error: code = Code(404) desc = offset out of range: 1", "grpc.code": "Code(404)", "grpc.time_ns": 337750}
github.com/grpc-ecosystem/go-grpc-middleware/logging/zap.DefaultMessageProducer
	/home/neo/go/pkg/mod/github.com/grpc-ecosystem/go-grpc-middleware@v1.3.0/logging/zap/options.go:212
github.com/grpc-ecosystem/go-grpc-middleware/logging/zap.UnaryServerInterceptor.func1
	/home/neo/go/pkg/mod/github.com/grpc-ecosystem/go-grpc-middleware@v1.3.0/logging/zap/server_interceptors.go:39
github.com/grpc-ecosystem/go-grpc-middleware.ChainUnaryServer.func1.1.1
	/home/neo/go/pkg/mod/github.com/grpc-ecosystem/go-grpc-middleware@v1.3.0/chain.go:25
github.com/grpc-ecosystem/go-grpc-middleware/tags.UnaryServerInterceptor.func1
	/home/neo/go/pkg/mod/github.com/grpc-ecosystem/go-grpc-middleware@v1.3.0/tags/interceptors.go:23
github.com/grpc-ecosystem/go-grpc-middleware.ChainUnaryServer.func1.1.1
	/home/neo/go/pkg/mod/github.com/grpc-ecosystem/go-grpc-middleware@v1.3.0/chain.go:25
github.com/grpc-ecosystem/go-grpc-middleware.ChainUnaryServer.func1
	/home/neo/go/pkg/mod/github.com/grpc-ecosystem/go-grpc-middleware@v1.3.0/chain.go:34
github.com/cedrickchee/commitlog/api/v1._Log_Consume_Handler
	/home/neo/dev/work/repo/github/commitlog/api/v1/log_grpc.pb.go:189
google.golang.org/grpc.(*Server).processUnaryRPC
	/home/neo/go/pkg/mod/google.golang.org/grpc@v1.41.0/server.go:1279
google.golang.org/grpc.(*Server).handleStream
	/home/neo/go/pkg/mod/google.golang.org/grpc@v1.41.0/server.go:1608
google.golang.org/grpc.(*Server).serveStreams.func1.2
	/home/neo/go/pkg/mod/google.golang.org/grpc@v1.41.0/server.go:923
2021/11/13 16:09:47 [DEBUG] serf: messageLeaveType: 0
2021/11/13 16:09:47 [DEBUG] serf: messageLeaveType: 0
...
2021/11/13 16:09:47 [INFO] serf: EventMemberLeave: 0 127.0.0.1
2021/11/13 16:09:47 [DEBUG] serf: messageLeaveType: 0
2021/11/13 16:09:47 [DEBUG] serf: messageLeaveType: 0
...
2021/11/13 16:09:47 [INFO] serf: EventMemberLeave: 0 127.0.0.1
2021-11-13T16:09:47.588+0800	DEBUG	membership	discovery/membership.go:165	failed to leave	{"error": "node is not the leader", "name": "0", "rpc_addr": "127.0.0.1:23314"}
2021/11/13 16:09:47 [INFO] serf: EventMemberLeave: 0 127.0.0.1
2021-11-13T16:09:47.588+0800	DEBUG	membership	discovery/membership.go:165	failed to leave	{"error": "node is not the leader", "name": "0", "rpc_addr": "127.0.0.1:23314"}
2021/11/13 16:09:48 [ERR] raft-net: Failed to accept connection: mux: server closed
2021-11-13T16:09:48.790+0800 [INFO]  raft: aborting pipeline replication to peer {Voter 1 127.0.0.1:23316}
2021-11-13T16:09:48.790+0800 [INFO]  raft: aborting pipeline replication to peer {Voter 2 127.0.0.1:23318}
2021/11/13 16:09:48 [DEBUG] serf: messageLeaveType: 1
2021/11/13 16:09:48 [DEBUG] serf: messageLeaveType: 1
...
2021/11/13 16:09:49 [INFO] serf: EventMemberLeave: 1 127.0.0.1
2021/11/13 16:09:49 [DEBUG] serf: messageLeaveType: 1
...
2021/11/13 16:09:49 [INFO] serf: EventMemberLeave: 1 127.0.0.1
2021-11-13T16:09:49.412+0800	DEBUG	membership	discovery/membership.go:165	failed to leave	{"error": "node is not the leader", "name": "1", "rpc_addr": "127.0.0.1:23316"}
2021/11/13 16:09:50 [DEBUG] serf: messageLeaveType: 1
2021-11-13T16:09:50.415+0800 [WARN]  raft: Heartbeat timeout from "[::]:23314" reached, starting election
2021-11-13T16:09:50.415+0800 [INFO]  raft: Node at [::]:23318 [Candidate] entering Candidate state in term 3
2021/11/13 16:09:50 [DEBUG] raft-net: [::]:23316 accepted connection from: 127.0.0.1:46614
2021-11-13T16:09:50.421+0800 [ERROR] raft: Failed to make RequestVote RPC to {Voter 0 [::]:23314}: dial tcp [::]:23314: connect: connection refused
2021-11-13T16:09:50.423+0800 [DEBUG] raft: Votes needed: 2
2021-11-13T16:09:50.423+0800 [DEBUG] raft: Vote granted from 2 in term 3. Tally: 1
2021-11-13T16:09:50.442+0800 [WARN]  raft: Rejecting vote request from [::]:23318 since we have a leader: [::]:23314
2021-11-13T16:09:50.917+0800 [WARN]  raft: Heartbeat timeout from "[::]:23314" reached, starting election
2021-11-13T16:09:50.917+0800 [INFO]  raft: Node at [::]:23316 [Candidate] entering Candidate state in term 3
2021-11-13T16:09:50.921+0800 [ERROR] raft: Failed to make RequestVote RPC to {Voter 0 [::]:23314}: dial tcp [::]:23314: connect: connection refused
2021-11-13T16:09:50.924+0800 [DEBUG] raft: Votes needed: 2
2021-11-13T16:09:50.924+0800 [DEBUG] raft: Vote granted from 1 in term 3. Tally: 1
2021/11/13 16:09:50 [DEBUG] raft-net: [::]:23318 accepted connection from: 127.0.0.1:39744
2021-11-13T16:09:50.943+0800 [INFO]  raft: Duplicate RequestVote for same term: 3
2021/11/13 16:09:51 [ERR] raft-net: Failed to accept connection: mux: server closed
2021/11/13 16:09:51 [INFO] serf: EventMemberLeave: 2 127.0.0.1
2021-11-13T16:09:51.927+0800 [WARN]  raft: Election timeout reached, restarting election
2021-11-13T16:09:51.928+0800 [INFO]  raft: Node at [::]:23318 [Candidate] entering Candidate state in term 4
2021/11/13 16:09:51 [ERR] raft-net: Failed to decode incoming command: transport shutdown
2021-11-13T16:09:51.932+0800 [ERROR] raft: Failed to make RequestVote RPC to {Voter 1 127.0.0.1:23316}: EOF
2021-11-13T16:09:51.933+0800 [ERROR] raft: Failed to make RequestVote RPC to {Voter 0 [::]:23314}: dial tcp [::]:23314: connect: connection refused
2021-11-13T16:09:51.935+0800 [DEBUG] raft: Votes needed: 2
2021-11-13T16:09:51.936+0800 [DEBUG] raft: Vote granted from 2 in term 4. Tally: 1
2021/11/13 16:09:51 [INFO] serf: EventMemberLeave: 1 127.0.0.1
2021/11/13 16:09:51 [INFO] serf: EventMemberFailed: 2 127.0.0.1
2021/11/13 16:09:52 [ERR] raft-net: Failed to accept connection: mux: server closed
--- PASS: TestAgent (13.06s)
PASS
ok  	command-line-arguments	13.088s
```

## Deployment

### Deploy Applications with Kubernetes Locally

We will deploy a cluster of our service. We will:

- Set up with Kubernetes and Helm so that we can orchestrate our service on both
  our local machine and later on a cloud platform.
- Run a cluster of your service on your machine.

**Install `kubectl`**

The Kubernetes command-line tool, `kubectl`, is used to run commands against
Kubernetes clusters.

If you're using Linux, you can install `kubectl` by referring to 
["Install and Set Up kubectl on Linux"](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)

We need a Kubernetes cluster and its API for `kubectl` to call and do anything.
We'll use the Kind tool to run a local Kubernetes cluster in Docker.

#### Use Kind for Local Development and Continuous Integration

[Kind](https://kind.sigs.k8s.io) is a tool developed by the Kubernetes team to
run local Kubernetes clusters using Docker containers as nodes.

To install Kind, run the following:

```sh
$ go get sigs.k8s.io/kind
go: downloading sigs.k8s.io/kind v0.11.1
go: downloading github.com/spf13/cobra v1.1.1
go: downloading github.com/alessio/shellescape v1.4.1
go: downloading golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c
go: downloading github.com/BurntSushi/toml v0.3.1
go: downloading github.com/evanphx/json-patch/v5 v5.2.0
go: downloading github.com/pelletier/go-toml v1.8.1
go: downloading gopkg.in/yaml.v2 v2.2.8
```

You can create a Kind cluster by running:

```sh
$ kind create cluster
Creating cluster "kind" ...
 ✓ Ensuring node image (kindest/node:v1.21.1) 🖼 
 ✓ Preparing nodes 📦  
 ✓ Writing configuration 📜 
 ✓ Starting control-plane 🕹️ 
 ✓ Installing CNI 🔌 
 ✓ Installing StorageClass 💾 
Set kubectl context to "kind-kind"
You can now use your cluster with:

kubectl cluster-info --context kind-kind

Thanks for using kind! 😊
```

You can then verify that Kind created your cluster and configured `kubectl` to
use it by running the following:

```sh
$ kubectl cluster-info
> Kubernetes control plane is running at https://127.0.0.1:36313
CoreDNS is running at https://127.0.0.1:36313/api/v1/namespaces/kube-system/services/kube-dns:dns/proxy
```

We have a running Kubernetes cluster now--let's run our service on it.

To run our service in Kubernetes, we'll need a Docker image, and our Docker
image will need an executable entry point. We've written an agent CLI that
serves as our service's executable.

### Build Your Docker Image

Build the image and load it into your Kind cluster by running:

```sh
$ make build-docker
docker build -t github.com/cedrickchee/commitlog:0.0.1 .
Sending build context to Docker daemon  1.341MB
Step 1/7 : FROM golang:1.17.3-alpine AS build
1.17.3-alpine: Pulling from library/golang
97518928ae5f: Pull complete 
b78c28b3bbf7: Pull complete 
248309d37e25: Pull complete 
c91f41641737: Pull complete 
e372233a5e04: Pull complete 
Digest: sha256:55da409cc0fe11df63a7d6962fbefd1321fedc305d9969da636876893e289e2d
Status: Downloaded newer image for golang:1.17.3-alpine
 ---> 3a38ce03c951
Step 2/7 : WORKDIR /go/src/commitlog
 ---> Running in 45cf5ec9d633
Removing intermediate container 45cf5ec9d633
 ---> 8e78d40c5f5d
Step 3/7 : COPY . .
 ---> 60bc04f1db2f
Step 4/7 : RUN CGO_ENABLED=0 go build -o /go/bin/commitlog ./cmd/commitlog
 ---> Running in 9ddbb287dae4
go: downloading github.com/spf13/cobra v1.2.1
go: downloading github.com/spf13/viper v1.9.0        
go: downloading github.com/hashicorp/raft v1.1.1
go: downloading github.com/soheilhy/cmux v0.1.5     
go: downloading go.uber.org/zap v1.19.1
...
go: downloading github.com/mattn/go-colorable v0.1.6
go: downloading github.com/hashicorp/errwrap v1.0.0
go: downloading golang.org/x/crypto v0.0.0-20210817164053-32db794688a5
Removing intermediate container 9ddbb287dae4
 ---> 6d2eb3145770
Step 5/7 : FROM scratch
 ---> 
Step 6/7 : COPY --from=build /go/bin/commitlog /bin/commitlog
 ---> bd5bc56b75bf
Step 7/7 : ENTRYPOINT ["/bin/commitlog"]
 ---> Running in 98d7271a2a24
Removing intermediate container 98d7271a2a24
 ---> 2d0f44d05f46
Successfully built 2d0f44d05f46
Successfully tagged github.com/cedrickchee/commitlog:0.0.1

$ kind load docker-image github.com/cedrickchee/commitlog:0.0.1
Image: "github.com/cedrickchee/commitlog:0.0.1" with ID "sha256:2d0f44d05f46ecbf6860bd5240bfbd90d4cf4814f3fd90c1bee3c75d7bb460bc" not yet present on node "kind-control-plane", loading...
```

### Configure and Deploy Your Service with Helm

Let's look at how we can configure and run a cluster of our service in
Kubernetes with Helm.

[Helm](https://helm.sh) is the package manager for Kubernetes that enables you
to distribute and install services in Kubernetes.

To [install Helm from script](https://helm.sh/docs/intro/install/#from-script),
run this command:

```sh
$ curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
$ chmod 700 get_helm.sh

# workaround script broke on linux: the https://github.com/helm/helm/issues/10266
$ export DESIRED_VERSION=v3.7.1
$ ./get_helm.sh
Downloading https://get.helm.sh/helm-v3.7.1-linux-amd64.tar.gz
Verifying checksum... Done.
Preparing to install helm into /usr/local/bin
helm installed into /usr/local/bin/helm
```

Now we're ready to deploy the service in our Kubernetes cluster.

#### Install Your Helm Chart

We can install our Helm chart in our Kind cluster to run a cluster of our service.

You can see what Helm renders by running:

```sh
$ helm template commitlog deploy/commitlog
```

Now, install the Chart by running this command:

```sh
$ helm install commitlog deploy/commitlog
NAME: commitlog
LAST DEPLOYED: Tue Nov 16 23:00:38 2021
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None
```

Wait a few seconds and you'll see Kubernetes set up three pods. You can list
them by running `$ kubectl get pods`. When all three pods are ready, we can try
requesting the API.

```sh
NAME          READY   STATUS    RESTARTS   AGE
commitlog-0   1/1     Running   0          3m37s
commitlog-1   1/1     Running   0          2m27s
commitlog-2   1/1     Running   0          77s
```

We can tell Kubernetes to forward a pod or a Service's port to a port on your
computer so you can request a service running inside Kubernetes without a load
balancer:

```sh
$ kubectl port-forward pod/commitlog-0 8500:8400
Forwarding from 127.0.0.1:8500 -> 8400
Forwarding from [::1]:8500 -> 8400
```

Now we can request our service from a program running outside Kubernetes at
`:8500`.

Run the command to request our service to get and print the list of servers:

```sh
$ go run cmd/getservers/main.go -addr=":8500"
```

You should see the following output:

```sh
servers:
- id:"commitlog-0" rpc_addr:"0.0.0.0:8400"
- id:"commitlog-1" rpc_addr:"0.0.0.0:8400"
- id:"commitlog-2" rpc_addr:"0.0.0.0:8400"
```

This means all three servers in our cluster have successfully joined the cluster
and are coordinating with each other.

#### Deploy Service with Kubernetes to the Cloud

1. Create a Google Kubernetes Engine (GKE) cluster
   - Sign up with Google Cloud
   - Create a Kubernetes cluster
   - Install and authenticate gcloud CLI
	 
	 Get your project's ID, and configure gcloud to use the project by default 
	 by running the following:

		```sh
		$ PROJECT_ID=$(gcloud projects list | tail -n 1 | cut -d' ' -f1)
		$ gcloud config set project $PROJECT_ID
		```

   - Push our service's image to Google's Container Registry (GCR)

		```sh
		$ gcloud auth configure-docker
		$ docker tag github.com/cedrickchee/commitlog:0.0.1 \
			gcr.io/$PROJECT_ID/commitlog:0.0.1
		$ docker push gcr.io/$PROJECT_ID/commitlog:0.0.1
		```

	- Configure kubectl

	  The last bit of setup allows kubectl and Helm to call our GKE cluster:

		```sh
		$ gcloud container clusters get-credentials commitlog --zone us-central1-a
		Fetching cluster endpoint and auth data.
		kubeconfig entry generated for commitlog.
		```

2. Install Metacontroller
   
   [Metacontroller](https://metacontroller.app) is a Kubernetes add-on that
   makes it easy to write and deploy custom controllers with simple scripts.

   Install the Metacontroller chart:

	```sh
	$ kubectl create namespace metacontroller
	$ helm install metacontroller metacontroller
	NAME: metacontroller
	LAST DEPLOYED: Sat Nov 20 23:33:42 2021
	NAMESPACE: default
	STATUS: deployed
	REVISION: 1
	TEST SUITE: None
	```

3. Deploy our service to our GKE cluster and try it.

Deploy our distributed service to the Cloud. Run the following command:

```sh
$ helm install commitlog commitlog \
--set image.repository=gcr.io/$PROJECT_ID/commitlog \
--set service.lb=true
```

You can watch as the services come up by passing the `-w` flag:

```sh
$ kubectl get services -w
NAME          TYPE           CLUSTER-IP     EXTERNAL-IP   PORT(S)                      AGE
commitlog     ClusterIP      None           <none>        8400/TCP,8401/TCP,8401/UDP   8m53s
commitlog-0   LoadBalancer   10.96.149.92   <pending>     8400:32735/TCP               8m25s
commitlog-1   LoadBalancer   10.96.15.175   <pending>     8400:30945/TCP               8m25s
commitlog-2   LoadBalancer   10.96.90.220   <pending>     8400:32552/TCP               8m25s
kubernetes    ClusterIP      10.96.0.1      <none>        443/TCP                      14m
```

When all three load balancers are up, we can verify that our client connects to
our service running in the Cloud and that our service nodes discovered each
other:

```sh
$ ADDR=$(kubectl get service -l app=service-per-pod -o go-template=\
'{{range .items}}\
{{(index .status.loadBalancer.ingress 0).ip}}{{"\n"}}\
{{end}}'\
| head -n 1)

$ go run cmd/getservers/main.go -addr=$ADDR:8400
servers:
- id:"commitlog-0" rpc_addr:"commitlog-0.commitlog.default.svc.cluster.local:8400"
- id:"commitlog-1" rpc_addr:"commitlog-1.commitlog.default.svc.cluster.local:8400"
- id:"commitlog-2" rpc_addr:"commitlog-2.commitlog.default.svc.cluster.local:8400"
```
