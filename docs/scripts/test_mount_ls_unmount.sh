#!/bin/bash

mount -o loop data/loop1.img /mnt/loop1
mkdir -p /mnt/loop1/test
ls /mnt/loop1
umount /mnt/loop1
