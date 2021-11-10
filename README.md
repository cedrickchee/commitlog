# commitlog

Commitlog is a distributed commit log service.

## Build a Log Package

Logs—which are sometimes also called _write-ahead logs_, _transaction logs_, or
_commit logs_—are at the heart of storage engines, message queues, version
control, and replication and consensus algorithms. As you build distributed
services, you'll face problems that you can solve with logs.

## Prerequisite

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

# Set Up

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

# Test

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