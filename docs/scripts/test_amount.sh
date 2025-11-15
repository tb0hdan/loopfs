#!/bin/bash

mkdir -p data
for i in $(seq 1 65535); do
    dd if=/dev/zero of=data/loop$i.img bs=1M count=1
    mkfs.ext4 data/loop$i.img
    mkdir -p /mnt/loop$i
    mount -o loop data/loop$i.img /mnt/loop$i
    if [ $? -ne 0 ]; then
	break
    fi
done

for target in $(mount|grep /mnt/loop|awk '{print $3}'); do umount $target; done
