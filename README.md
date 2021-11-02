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
2021/11/02 20:49:49 [INFO] generating a new CA key and certificate from CSR
2021/11/02 20:49:49 [INFO] generate received request
2021/11/02 20:49:49 [INFO] received CSR
2021/11/02 20:49:49 [INFO] generating key: rsa-2048
2021/11/02 20:49:50 [INFO] encoded CSR
2021/11/02 20:49:50 [INFO] signed certificate with serial number 609991520964339114875917627510871516675247155359

# Generating self-signed server certificate and private key
cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=server \
		test/server-csr.json | cfssljson -bare server
2021/11/02 20:49:50 [INFO] generate received request
2021/11/02 20:49:50 [INFO] received CSR
2021/11/02 20:49:50 [INFO] generating key: rsa-2048
2021/11/02 20:49:50 [INFO] encoded CSR
2021/11/02 20:49:50 [INFO] signed certificate with serial number 722518663172071887972604384793191499654476226861

# Generating cert and private key for your client
cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=client \
		test/client-csr.json | cfssljson -bare client
2021/11/02 20:49:50 [INFO] generate received request
2021/11/02 20:49:50 [INFO] received CSR
2021/11/02 20:49:50 [INFO] generating key: rsa-2048
2021/11/02 20:49:50 [INFO] encoded CSR
2021/11/02 20:49:50 [INFO] signed certificate with serial number 713475798219442340274518763748614246528384531621

mv *.pem *.csr /home/neo/dev/work/repo/github/commitlog/.config
```
