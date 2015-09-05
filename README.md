What is it
=============
A simple API for managing Bhyve instances, useful for building a FreeBSD based
infrastructure as a service

Done So Far
================
  + api server (go based) that can create new instances based on existing golden images
    + launch instances
    + stop instances
    + terminate/destroy instances
    + puts config data into zfs properties
    + support zvol images
    + support FreeBSD guests

Demo
=========
[![asciicast](https://asciinema.org/a/dvre3aezrs8fascnvj3a1jijx.png)](https://asciinema.org/a/dvre3aezrs8fascnvj3a1jijx)

ToDo
================
  - api server
    - create new instances based on snapshots of existing instances
    - basically, AWS EC2, Digital Ocean, vmware to a degree, entirely on-prem
    - config file for network interfaces?
    - serial stuff
    - read iohyve/iocage for inspiration
    - support disk file based images
    - support iscsi images
    - support nfsroot images
    - support Linux guests
    - support Windows guest
  - meta data server (go based) that services requests for configuration data from cloud init clients
    - serve correct instance data to instance based on zfs properties
  - cli interface to make requests to api server
    - allows settings per instance data
  - gui to make requests to api server
  - automated way to create images
  - some way to manage the instances across nodes
  - control access between instances


Things to reference for TODO
===================================
* [Amazon API](http://docs.aws.amazon.com/AWSEC2/latest/APIReference/OperationList-query.html)
* Digital Ocean API
* Google Cloud API
* Xen API

How to use it currently
===================================

* install FreeBSD 11-CURRENT
* setup zfs
* follow the steps in the [handbook](https://www.freebsd.org/doc/handbook/virtualization-host-bhyve.html) to install a bhyve VM, but instead of using a disk file, use a zvol. This [section](https://www.freebsd.org/doc/handbook/zfs-zfs.html) of the handbook should help.
* create a snapshot of the installed zvol called bhyve01@2015081817020001
* create a clone of that snapshot called ima-<8hexdigits> and create a snapshot of that called ima-<8hexdigits>@0
* install [ttyrec](http://www.freshports.org/misc/ttyrec): `pkg install ttyrec`
* install [curl](http://www.freshports.org/ftp/curl/): `pkg install curl`
* install [go](http://www.freshports.org/lang/go): `pkg install go`
* install [gotty](https://github.com/yudai/gotty)
* clone this repo
* setup GOPATH to point to this directory
* `go get github.com/ant0ine/go-json-rest`
* `go get github.com/mattn/go-getopt`
* `go get github.com/satori/go.uuid`
* go run pangolin-api.go -z <zpoolname> -i <netinterface>
* list images, create instances, etc. (see below)
* connect to them in the web browser

Making requests
=====================

## list images ##
`curl -i http://127.0.0.1:8080/api/v1/images`

## launch instance from image (creates and starts) ##
`curl -i -H 'Content-Type: application/json' -d '{"ima": "<imageid>", "mem": 512, "cpu": 1}' http://127.0.0.1:8080/api/v1/instances`

## list instances ##
`curl -i http://127.0.0.1:8080/api/v1/instances`

## stop instance ##
`curl -i -X PUT http://127.0.0.1:8080/api/v1/instances/<instanceid>`

## start instance ##
``curl -i -X POST http://127.0.0.1:8080/api/v1/instances/<instanceid>`

## delete instance ##
`curl -i -X DELETE http://127.0.0.1:8080/api/v1/instances/<instanceid>`
