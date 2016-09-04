#!/usr/bin/env bash

apt-get update
apt-get install -y golang
cd /vagrant
go build -o httpfile
cp httpfile /home/vagrant/

cp httpfile-upstart.conf /etc/init
initctl reload-configuration
