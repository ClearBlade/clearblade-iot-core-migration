#!/bin/bash

OUTPUT_FILE="devicesToMigrate_20k.csv"

echo "deviceId" > "$OUTPUT_FILE"
for i in $(seq 1 20000); do
    echo "device-$i" >> "$OUTPUT_FILE"
done
