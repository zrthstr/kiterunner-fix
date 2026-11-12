# Kiterunner

![](/hack/kiterunner.png)

[![Go](https://github.com/zrthstr/kiterunner-fix/actions/workflows/go-build-test.yml/badge.svg)](https://github.com/zrthstr/kiterunner-fix/actions/workflows/go-build-test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zrthstr/kiterunner-fix.svg)](https://pkg.go.dev/github.com/zrthstr/kiterunner-fix)

> **This is a maintenance fork of [assetnote/kiterunner](https://github.com/assetnote/kiterunner).**
> Kiterunner is Assetnote's work and all credit for it goes to them. The original has not been updated since
> 2021; this fork only tries to keep it working with today's Go, dependencies and Assetnote wordlists, and to
> fix the bugs that got in the way. It is not a rewrite and adds no new scanning features.
>
> What changed, in short:
>
> - **Bugs that cost results:** valid hits were silently dropped (a 200 compared against a 404 wildcard
>   baseline, baselines leaking between targets, short 401/403 pages discarded as wildcards)
> - **Crashes and hangs:** scanning targets from stdin, `kb replay`, a deadlock in the scan engine, data races
> - **Flags that did nothing:** `--delay`, `--force-method`, `--config`, `brute`'s default depth, `--no-cache`;
>   `--timeout` was effectively 5x longer against unresponsive hosts
> - **Assetnote wordlists:** the current lists work again, including the official `routes-small` and
>   `routes-large` kite files with `-A`; failed downloads are no longer cached
> - **Maintenance:** current Go and dependencies, `go install` support, tests that check what kr finds
>   (false positives and false negatives) against realistic servers, and a hardened CI
>
> See the [commit history](https://github.com/zrthstr/kiterunner-fix/commits/main) for the details.

# Introduction

For the longest of times, content discovery has been focused on finding files and folders. While this approach is effective for legacy web servers that host static files or respond with 3xx’s upon a partial path, it is no longer effective for modern web applications, specifically APIs.

Over time, we have seen a lot of time invested in making content discovery tools faster so that larger wordlists can be used, however the art of content discovery has not been innovated upon.

Kiterunner is a tool that is capable of not only performing traditional content discovery at lightning fast speeds, but also bruteforcing routes/endpoints in modern applications.

Modern application frameworks such as Flask, Rails, Express, Django and others follow the paradigm of explicitly defining routes which expect certain HTTP methods, headers, parameters and values. 

When using traditional content discovery tooling, such routes are often missed and cannot easily be discovered.

By collating a dataset of Swagger specifications and condensing it into our own schema, Kiterunner can use this dataset to bruteforce API endpoints by sending the correct HTTP method, headers, path, parameters and values for each request it sends.

Swagger files were collected from a number of datasources, including an internet wide scan for the 40+ most common swagger paths. Other datasources included [GitHub via BigQuery](https://cloud.google.com/bigquery/public-data/github), and [APIs.guru](https://apis.guru/).

# Contents

* [Kiterunner](#kiterunner)
* [Introduction](#introduction)
* [Installation](#installation)
  * [Downloading a release](#downloading-a-release)
  * [Building from source](#building-from-source)
  * [Installing via AUR](#aur)
* [Usage](#usage)
  * [Quick Start](#quick-start)
  * [CLI Help](#cli-help)
  * [Input/Host Formatting](#inputhost-formatting)
  * [API Scanning](#api-scanning)
  * [Vanilla Bruteforcing](#vanilla-bruteforcing)
  * [Dirsearch Bruteforcing](#dirsearch-bruteforcing)
* [Technical Features](#technical-features)
  * [Depth Scanning](#depth-scanning)
  * [Using Assetnote Wordlists](#using-assetnote-wordlists)
    * [Head Syntax](#head-syntax)
  * [Concurrency Settings/Going Fast](#concurrency-settingsgoing-fast)
  * [Converting between file formats](#converting-between-file-formats)
  * [Replaying requests](#replaying-requests)
* [Technical Implementation](#technical-implementation)
  * [Intermediate Data Type (PRoutes)](#intermediate-data-type-proutes)
  * [Kite File Format](#kite-file-format)

# Installation

## Installing with Go

```bash
go install github.com/zrthstr/kiterunner-fix/cmd/kiterunner@latest

# the binary is named after its directory; the docs below call it kr
ln -s "$(go env GOPATH)/bin/kiterunner" /usr/local/bin/kr
```

## Downloading a release

Pre-built copies of the original are at https://github.com/assetnote/kiterunner/releases.
Note that these do not contain the fixes from this fork.

## Building from source
```bash
# build the binary
make build

# run the tests. the engine tests need the bundled test server, otherwise they are skipped
make bench-up &   # serves ports 14000-15001
make test

# symlink your binary
ln -s $(pwd)/dist/kr /usr/local/bin/kr

# scan away. -A downloads and caches Assetnote's compiled routes wordlist on first use
kr scan hosts.txt -A routes-small -x 20 -j 100 --ignore-length=1053
```

`routes-small` and `routes-large` are Assetnote's compiled `.kite` wordlists. `-A` fetches them for you, but you
can also download them and use them with `-w`, or build your own `.kite` from the JSON datasets with
`kr kb compile routes.json routes.kite`.

The JSON datasets can be found below:

- [routes-large.json](https://wordlists-cdn.assetnote.io/rawdata/kiterunner/routes-large.json.tar.gz) (118MB compressed, 2.6GB decompressed)
- [routes-small.json](https://wordlists-cdn.assetnote.io/rawdata/kiterunner/routes-small.json.tar.gz) (14MB compressed, 228MB decompressed)

Alternatively, it is possible to download the compile `.kite` files from the links below:

- [routes-large.kite](https://wordlists-cdn.assetnote.io/data/kiterunner/routes-large.kite.tar.gz) (35MB compressed, 183M decompressed), or `-A routes-large`
- [routes-small.kite](https://wordlists-cdn.assetnote.io/data/kiterunner/routes-small.kite.tar.gz) (430KB compressed, 2MB decompressed), or `-A routes-small`

## AUR
Users using a Arch based distro can download the pre-built binary from [AUR](https://aur.archlinux.org/packages/kiterunner-bin/)
You can use a "Aur Helper" like `yay` to install kiterunner
```
yay -S kiterunner-bin
```
# Usage

## Quick Start

```
kr [scan|brute] <input> [flags]
```

- `<input>` can be a file, a domain, or URI. we'll figure it out for you. See  [Input/Host Formatting](#inputhost-formatting) for more details

```
# Just have a list of hosts and no wordlist: use Assetnote's compiled API routes
kr scan hosts.txt -A=routes-small -x 5 -j 100 --fail-status-codes 400,401,404,403,501,502,426,411

# Or the monthly generated API routes list from HTTP Archive, first 20000 lines
kr scan hosts.txt -A=apiroutes-260227:20000 -x 5 -j 100 --fail-status-codes 400,401,404,403,501,502,426,411

# You have your own wordlist but you want assetnote wordlists too
kr scan target.com -w routes.kite -A=apiroutes-260227:20000 -x 20 -j 1 --fail-status-codes 400,401,404,403,501,502,426,411

# Bruteforce like normal but with the first 20000 words
kr brute https://target.com/subapp/ -A=aspx-260227:20000 -x 20 -j 1

# Use a dirsearch style wordlist with %EXT%
kr brute https://target.com/subapp/ -w dirsearch.txt -x 20 -j 1 -exml,asp,aspx,ashx -D
```



## CLI Help

```
Usage:
  kite scan [flags]

Flags:
  -A, --assetnote-wordlist strings    use the wordlists from wordlist.assetnote.io. specify the type/name to use, e.g. apiroutes-260227. You can specify an additional maxlength to use only the first N values in the wordlist, e.g. apiroutes-260227:20000 will only use the first 20000 lines in that wordlist
      --blacklist-domain strings      domains that are blacklisted for redirects. We will not follow redirects to these domains
      --delay duration                delay to place inbetween requests to a single host
      --disable-precheck              whether to skip host discovery
      --fail-status-codes ints        which status codes blacklist as fail. if this is set, this will override success-status-codes
      --filter-api strings            only scan apis matching this ksuid
      --force-method string           whether to ignore the methods specified in the ogl file and force this method
  -H, --header strings                headers to add to requests (default [x-forwarded-for: 127.0.0.1])
  -h, --help                          help for scan
      --ignore-length strings         a range of content length bytes to ignore. you can have multiple. e.g. 100-105 or 1234 or 123,34-53. This is inclusive on both ends
      --kitebuilder-full-scan         perform a full scan without first performing a phase scan.
  -w, --kitebuilder-list strings      ogl wordlist to use for scanning
  -x, --max-connection-per-host int   max connections to a single host (default 3)
  -j, --max-parallel-hosts int        max number of concurrent hosts to scan at once (default 50)
      --max-redirects int             maximum number of redirects to follow (default 3)
  -d, --preflight-depth int           when performing preflight checks, what directory depth do we attempt to check. 0 means that only the docroot is checked (default 1)
      --profile-name string           name for profile output file
      --progress                      a progress bar while scanning. by default enabled only on Stderr (default true)
      --quarantine-threshold int      if the host return N consecutive hits, we quarantine the host as wildcard. Set to 0 to disable (default 10)
      --success-status-codes ints     which status codes whitelist as success. this is the default mode
  -t, --timeout duration              timeout to use on all requests (default 3s)
      --user-agent string             user agent to use for requests (default "Chrome. Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.96 Safari/537.36")
      --wildcard-detection            can be set to false to disable wildcard redirect detection (default true)

Global Flags:
      --config string    config file (default is $HOME/.kiterunner.yaml)
  -o, --output string    output format. can be json,text,pretty (default "pretty")
  -q, --quiet            quiet mode. will mute unecessarry pretty text
  -v, --verbose string   level of logging verbosity. can be error,info,debug,trace (default "info")
```

bruteforce flags (all the flags above +)
```
  -D, --dirsearch-compat              this will replace %EXT% with the extensions provided. backwards compat with dirsearch because shubs loves him some dirsearch
  -e, --extensions strings            extensions to append while scanning
  -w, --wordlist strings              normal wordlist to use for scanning
```
## Input/Host Formatting

When supplied with an input, kiterunner will attempt to resolve the input in the following order:
1. Is the input a file. If so read all the lines in the file as separate domains
2. The input is treated as a "domain"

If you supply a "domain", but it exists as a file, e.g. `google.com` but `google.com` is also a txt file in the current directory,
we'll load `google.com` the text file, because we found it first.

**Domain Parsing**

Its preferred that you provide a full URI as the input, however you can provide incomplete URIs and we'll try and guess what you mean.
An example list of domains you can supply are:

```
one.com
two.com:80
three.com:443
four.com:9447
https://five.com:9090
http://six.com:80/api
```

The above list of domains will expand into the subsequent list of targets

```
(two targets are created for one.com, since neither port nor protocol was specified)
http://one.com (port 80 implied)
https://one.com (port 443 implied)

http://two.com (port 80 implied)
https://three.com (port 443 implied)
http://four.com:9447 (non-tls port guessed)
https://five.com:9090
http://six.com/api (port 80 implied; basepath API appended)
```

the rules we apply are:
- if you supply a scheme, we use the scheme.
  - We only support http & https
  - if you don't supply a scheme, we'll guess based on the port
- if you supply a port, we'll use the port
  - If your port is 443, or 8443, we'll assume its tls
  - if you don't supply a port, we'll guess both port 80, 443
- if you supply a path, we'll prepend that path to all requests against that host

## API Scanning

When you have a single target
```bash
# single target
kr scan https://target.com:8443/ -w routes.kite -A=apiroutes-260227:20000 -x 10 --ignore-length=34

# single target, but you want to try http and https
kr scan target.com -w routes.kite -A=apiroutes-260227:20000 -x 10 --ignore-length=34

# a list of targets
kr scan targets.txt -w routes.kite -A=apiroutes-260227:20000 -x 10 --ignore-length=34
```

## Vanilla Bruteforcing 

```bash
kr brute https://target.com -A=raft-large-words -A=apiroutes-260227:20000 -x 10 -d=0 --ignore-length=34 -ejson,txt
```

## Dirsearch Bruteforcing

For when you have an old-school wordlist that still has %EXT% in the wordlist, you can use `-D`. this will only substitute the extension where %EXT% is present in the path

```bash
kr brute https://target.com -w dirsearch.txt -x 10 -d=0 --ignore-length=34 -ejson,txt -D
```

# Technical Features

## Depth Scanning

A key feature of kiterunner is depth based scanning. This attempts to handle detecting wildcards given virtual application path based routing. The depth defines how many directories deep the baseline checks are performed E.g.

```bash
~/kiterunner $ cat wordlist.txt

/api/v1/user/create
/api/v1/user/delete
/api/v2/user/
/api/v2/admin/
/secrets/v1/
/secrets/v2/
```

- At depth 0, only `/` would have the baseline checks performed for wildcard detection
- At depth 1, `/api` and `/secrets` would have baseline checks performed; and these checks would be used against `/api` and `/secrets` correspondingly
- At depth 2, `/api/v1`, `/api/v2`, `/secrets/v1` and `/secrets/v2` would all have baseline checks performed.

By default, `kr scan` has a depth of 1, since from internal usage, we've often seen this as the most common depth where virtual routing has occured. `kr brute` has a default depth of 0, as you typically don't want this check to be performed with a static wordlist.

Naturally, increasing the depth will increase the accuracy of your scans, however this also increases the number of requests to the target. (`# of baseline checks * # of depth baseline directories`). Hence, we recommend against going above 1, and in rare cases going to depth 2.


## Using Assetnote Wordlists

Kiterunner downloads and caches the wordlists published at [wordlists.assetnote.io](https://wordlists.assetnote.io/).
Use them with the `-A` flag, which takes a comma delimited list of aliases or full filenames. `kr wordlist list`
shows all of them; `kr wordlist save <name>` downloads one ahead of time (`--no-cache` to fetch it again).

- **Automatically generated** (`automated.json`): generated monthly from HTTP Archive. The aliases carry the date,
  e.g. `apiroutes-260227`, and change when Assetnote publishes new lists, so check `kr wordlist list` for the
  current names.
- **Manually generated** (`manual.json`): e.g. `raft-large-words`, `bak`, `dot_filenames`.
- **Kiterunner** (`kiterunner.json`): `routes-small` and `routes-large` are compiled `.kite` wordlists for
  `kr scan`; `swagger-wordlist` is a plain wordlist for `kr brute`.
- The **technology to host mappings** on the site are lists of hosts, not wordlists. Download them from the site
  and pass them as the input to `kr scan`.

Wordlists are cached in `~/.cache/kiterunner/wordlists` (or `$XDG_CACHE_HOME/kiterunner/wordlists`). Plain
wordlists used with `kr scan` are compiled from `.txt` to `.kite` on first use. Set `KITERUNNER_WORDLIST_URL`
to use a mirror of `https://wordlists.assetnote.io/data/`.

```
+-----------------------------------+-------------------------------------------------------+-----------------+---------+----------+--------+
|               ALIAS               |                       FILENAME                        |     SOURCE      |  COUNT  | FILESIZE | CACHED |
+-----------------------------------+-------------------------------------------------------+-----------------+---------+----------+--------+
| bak                               | bak.txt                                               | manual.json     | 31725   | 634.8kb  | false  |
| dot_filenames                     | dot_filenames.txt                                     | manual.json     | 3191712 | 71.3mb   | false  |
| apiroutes-260227                  | httparchive_apiroutes_2026_02_27.txt                  | automated.json  | 364056  | 10.7mb   | false  |
| aspx-260227                       | httparchive_aspx_asp_cfm_svc_ashx_asmx_2026_02_27.txt | automated.json  | 80989   | 1.7mb    | false  |
| directories-260227                | httparchive_directories_1m_2026_02_27.txt             | automated.json  | 703323  | 19.5mb   | false  |
| js-260227                         | httparchive_js_2026_02_27.txt                         | automated.json  | 3292760 | 61.7mb   | false  |
| php-260227                        | httparchive_php_2026_02_27.txt                        | automated.json  | 177015  | 3.3mb    | false  |
| raft-large-words                  | raft-large-words.txt                                  | manual.json     | 119600  | 1.0mb    | false  |
| routes-large                      | routes-large.kite.tar.gz                              | kiterunner.json | 139983  | 34.7mb   | false  |
| routes-small                      | routes-small.kite.tar.gz                              | kiterunner.json | 1537    | 429.9kb  | false  |
| swagger-wordlist                  | swagger-wordlist.txt                                  | kiterunner.json | 958872  | 27.2mb   | false  |
<SNIP>
+-----------------------------------+-------------------------------------------------------+-----------------+---------+----------+--------+
```

**Usage**
```
kr scan targets.txt -A=apiroutes-260227 -x 10 --ignore-length=34
kr brute targets.txt -A=aspx-260227 -x 10 --ignore-length=34 -easp,aspx
```

### Head Syntax

When using assetnote provided wordlists, you may not want to use the entire wordlist, so you can opt to use the first N lines in a given wordlist using the `head syntax`. The format is `<wordlist_name>:<N lines>` when specifying a wordlist.

**Usage**
```
# this will use the first 20000 lines in the api routes wordlist
kr scan targets.txt -A=apiroutes-260227:20000 -x 10 --ignore-length=34

# this will use the first 10 lines in the aspx wordlist
kr brute targets.txt -A=aspx-260227:10 -x 10 --ignore-length=34 -easp,aspx
```

## Concurrency Settings/Going Fast

Kiterunner is made to go fast on a lot of hosts. But, just because you can run kiterunner at 20000 goroutines, doesn't mean its a good idea. Bottlenecks and performance degredation will occur at high thread counts due to more time spent scheduling goroutines that are waiting on network IO and kernel context switching.

There are two main concurrency settings for kiterunner:
- `-x, --max-connection-per-host` - maximum number of open connections we can have on a host. Governed by 1 goroutine each. To avoid DOS'ing a host, we recommend keeping this in a low realm of 5-10. Depending on latency to the target, this will yield on average between 1-5 requests per second per connection (200ms - 1000ms/req) to a host.
- `-j, --max-parallel-hosts` - maximum number of hosts to scan at any given time. Governed by 1 goroutine supervisor for each

Depending on the hardware you are scanning from, the "maximum" number of goroutines you can run optimally will vary. On an AWS t3.medium, we saw performance degradation going over 2500 goroutines. Meaning, 500 hosts x 5 conn per host (2500) would yield peak performance.

We recommend **against** running kiterunner from your **macbook**. Due to poor kernel optimisations for high IO counts and Epoll syscalls on macOS, we noticed substantially poorer (0.3-0.5x) performance when compared to running kiterunner on a similarly configured linux instance.

To maximise performance when scanning an individual target, or a large attack surface we recommend the following tips:
- Spin up an EC2 instance in a similar geographic region/datacenter to the target(s) you are scanning
- Perform some initial benchmarks against your target set with varying `-x` and `-j` options. We recommend having a typical starting point of around `-x 5 -j 100` and moving `-j` upwards as your CPU usage/network performance permits

## Converting between file formats

Kiterunner will also let you convert between the schema JSON, a kite file and a standard txt wordlist. 

**Usage**

The format is decided by the filetype extension supplied by the `<input>` and `<output>` fields. We support `txt`, `json` and `kite`
```bash
kr kb convert wordlist.txt wordlist.kite
kr kb convert wordlist.kite wordlist.json
kr kb convert wordlist.kite wordlist.txt
```
```
❯ go run ./cmd/kiterunner kb convert -qh
convert an input file format into the specified output file format

this will determine the conversion based on the extensions of the input and the output
we support the following filetypes: txt, json, kite
You can convert any of the following into the corresponding types

-d Debug mode will attempt to convert the schema with error handling
-v=debug Debug verbosity will print out the errors for the schema

Usage:
kite kb convert <input> <output> [flags]

Flags:
-d, --debug   debug the parsing
-h, --help    help for convert

Global Flags:
--config string    config file (default is $HOME/.kiterunner.yaml)
-o, --output string    output format. can be json,text,pretty (default "pretty")
-q, --quiet            quiet mode. will mute unecessarry pretty text
-v, --verbose string   level of logging verbosity. can be error,info,debug,trace (default "info")``bigquery
```

## Replaying requests

When you receive a bunch of output from kiterunner, it may be difficult to immediately understand why a request is causing a specific response code/length. Kiterunner offers a method of rebuilding the request from the wordlists used including all the header and body parameters.

- You can replay a request by copy pasting the full response output into the `kb replay` command. 
- You can specify a `--proxy` to forward your requests through, so you can modify/repeat/intercept the request using 3rd party tools if you wish
- The golang net/http client will perform a few additional changes to your request due to how the default golang spec implementation (unfortunately).

```bash
❯ go run ./cmd/kiterunner kb replay -q --proxy=http://localhost:8080 -w routes.kite "POST    403 [    287,   10,   1] https://target.com/dedalo/lib/dedalo/publication/server_api/v1/json/thesaurus_parents 0cc39f76702ea287ec3e93f4b4710db9c8a86251"
11:25AM INF Raw reconstructed request
POST /dedalo/lib/dedalo/publication/server_api/v1/json/thesaurus_parents?ar_fields=48637466&code=66132381&db_name=08791392&lang=lg-eng&recursive=false&term_id=72336471 HTTP/1.1
Content-Type: any


11:25AM INF Outbound request
POST /dedalo/lib/dedalo/publication/server_api/v1/json/thesaurus_parents?ar_fields=48637466&code=66132381&db_name=08791392&lang=lg-eng&recursive=false&term_id=72336471 HTTP/1.1
Host: target.com
User-Agent: Go-http-client/1.1
Content-Length: 0
Content-Type: any
Accept-Encoding: gzip


11:25AM INF Response After Redirects
HTTP/1.1 403 Forbidden
Connection: close
Content-Length: 45
Content-Type: application/json
Date: Wed, 07 Apr 2021 01:25:28 GMT
X-Amzn-Requestid: 7e6b2ea1-c662-4671-9eaa-e8cd31b463f2

User is not authorized to perform this action
```

# Technical Implementation

## Intermediate Data Type (PRoutes)

We use an intermediate representation of wordlists and kitebuilder json schemas in kiterunner. This is to allow us to dynamically generate the fields in the wordlist and reconstruct request bodies/headers and query parameters from a given spec.

The PRoute type is composed of Headers, Body, Query and Cookie parameters that are encoded in `pkg/proute.Crumb`. The Crumb type is an interface that is implemented on types such as UUIDs, Floats, Ints, Random Strings, etc.

When performing conversions to and from txt, json and kite files, all the conversions are first done to the `proute.API` intermediate type. Then the corresponding encoding is written out

## Kite File Format

We use a super secret kite file format for storing the json schemas from kitebuilder. These are simply protobuf encoded `pkg/proute.APIS` written to a file. The compilation is used to allow us to quickly deserialize the already parsed wordlist. This file format is not stable, and should only be interacted with using the inbuilt conversion tools for kiterunner.

When a new version of the kite file format is released, you may need to recompile your kite files
