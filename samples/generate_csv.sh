#!/bin/bash

OUTPUT_FILE="devicesToMigrate_10k.csv"

echo "deviceId" > "$OUTPUT_FILE"
for i in $(seq 1 10000); do
    echo "device-$i" >> "$OUTPUT_FILE"
done
