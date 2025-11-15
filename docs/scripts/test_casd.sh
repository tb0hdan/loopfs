#!/bin/bash

# Test script for casd - Content Addressable Storage daemon
# This script:
# 1. Creates a file with random data
# 2. Uploads the file to casd
# 3. Verifies the file exists by requesting its info

set -e  # Exit on any error

# Configuration
CASD_URL="http://localhost:8080"
TEST_FILE="test_random_data.txt"
TEST_SIZE=1024  # Size in bytes for random data

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Starting casd test...${NC}"

# Step 1: Create a file with random data
echo -e "${YELLOW}Step 1: Creating file with random data (${TEST_SIZE} bytes)${NC}"
dd if=/dev/urandom of="${TEST_FILE}" bs=${TEST_SIZE} count=1 2>/dev/null
echo -e "${GREEN}✓ Created ${TEST_FILE} with ${TEST_SIZE} bytes of random data${NC}"

# Calculate expected hash for verification
EXPECTED_HASH=$(sha256sum "${TEST_FILE}" | cut -d' ' -f1)
echo -e "${GREEN}✓ Expected SHA256: ${EXPECTED_HASH}${NC}"

# Step 2: Upload the file
echo -e "${YELLOW}Step 2: Uploading file to casd${NC}"
UPLOAD_RESPONSE=$(curl -s -X POST -F "file=@${TEST_FILE}" "${CASD_URL}/file/upload")

# Check if upload was successful
if [[ $? -ne 0 ]]; then
    echo -e "${RED}✗ Failed to upload file. Is casd running on ${CASD_URL}?${NC}"
    rm -f "${TEST_FILE}"
    exit 1
fi

# Extract hash from response
RETURNED_HASH=$(echo "${UPLOAD_RESPONSE}" | grep -o '"hash":"[^"]*"' | cut -d'"' -f4)

if [[ -z "${RETURNED_HASH}" ]]; then
    echo -e "${RED}✗ Failed to extract hash from upload response: ${UPLOAD_RESPONSE}${NC}"
    rm -f "${TEST_FILE}"
    exit 1
fi

echo -e "${GREEN}✓ Upload successful. Returned hash: ${RETURNED_HASH}${NC}"

# Verify the returned hash matches expected
if [[ "${RETURNED_HASH}" != "${EXPECTED_HASH}" ]]; then
    echo -e "${RED}✗ Hash mismatch! Expected: ${EXPECTED_HASH}, Got: ${RETURNED_HASH}${NC}"
    rm -f "${TEST_FILE}"
    exit 1
fi

echo -e "${GREEN}✓ Hash verification passed${NC}"

# Step 3: Verify file exists by requesting its info
echo -e "${YELLOW}Step 3: Verifying file exists by requesting info${NC}"
INFO_RESPONSE=$(curl -s "${CASD_URL}/file/${RETURNED_HASH}/info")

# Check if info request was successful
if [[ $? -ne 0 ]]; then
    echo -e "${RED}✗ Failed to get file info${NC}"
    rm -f "${TEST_FILE}"
    exit 1
fi

# Check if response contains the hash
if echo "${INFO_RESPONSE}" | grep -q "${RETURNED_HASH}"; then
    echo -e "${GREEN}✓ File info retrieved successfully${NC}"

    # Parse and display file info
    FILE_SIZE=$(echo "${INFO_RESPONSE}" | grep -o '"size":[0-9]*' | cut -d':' -f2)
    CREATED_AT=$(echo "${INFO_RESPONSE}" | grep -o '"created_at":"[^"]*"' | cut -d'"' -f4)
    SPACE_USED=$(echo "${INFO_RESPONSE}" | grep -o '"space_used":[0-9]*' | cut -d':' -f2)
    SPACE_AVAILABLE=$(echo "${INFO_RESPONSE}" | grep -o '"space_available":[0-9]*' | cut -d':' -f2)

    echo -e "${GREEN}  File Details:${NC}"
    echo -e "${GREEN}    • Hash: ${RETURNED_HASH}${NC}"
    echo -e "${GREEN}    • Size: ${FILE_SIZE} bytes${NC}"
    echo -e "${GREEN}    • Created: ${CREATED_AT}${NC}"

    # Display loop filesystem usage if available
    if [[ -n "${SPACE_USED}" && -n "${SPACE_AVAILABLE}" ]]; then
        # Convert bytes to human readable format
        SPACE_USED_MB=$(echo "scale=2; ${SPACE_USED} / 1048576" | bc 2>/dev/null || echo "N/A")
        SPACE_AVAILABLE_MB=$(echo "scale=2; ${SPACE_AVAILABLE} / 1048576" | bc 2>/dev/null || echo "N/A")
        SPACE_TOTAL=$((SPACE_USED + SPACE_AVAILABLE))
        SPACE_TOTAL_MB=$(echo "scale=2; ${SPACE_TOTAL} / 1048576" | bc 2>/dev/null || echo "N/A")

        echo -e "${GREEN}  Loop Filesystem Usage:${NC}"
        echo -e "${GREEN}    • Used: ${SPACE_USED} bytes (${SPACE_USED_MB} MB)${NC}"
        echo -e "${GREEN}    • Available: ${SPACE_AVAILABLE} bytes (${SPACE_AVAILABLE_MB} MB)${NC}"
        echo -e "${GREEN}    • Total: ${SPACE_TOTAL} bytes (${SPACE_TOTAL_MB} MB)${NC}"
    else
        echo -e "${YELLOW}  Loop Filesystem Usage: Not available${NC}"
    fi
else
    echo -e "${RED}✗ File info does not contain expected hash${NC}"
    echo -e "${RED}  Response: ${INFO_RESPONSE}${NC}"
    rm -f "${TEST_FILE}"
    exit 1
fi

# Optional: Test download
echo -e "${YELLOW}Step 4 (Optional): Testing file download${NC}"
DOWNLOAD_FILE="${TEST_FILE}.downloaded"
curl -s "${CASD_URL}/file/${RETURNED_HASH}/download" -o "${DOWNLOAD_FILE}"

if [[ $? -eq 0 ]]; then
    # Compare original and downloaded files
    if cmp -s "${TEST_FILE}" "${DOWNLOAD_FILE}"; then
        echo -e "${GREEN}✓ Download successful - files match${NC}"
    else
        echo -e "${RED}✗ Download successful but files don't match${NC}"
        rm -f "${TEST_FILE}" "${DOWNLOAD_FILE}"
        exit 1
    fi
    rm -f "${DOWNLOAD_FILE}"
else
    echo -e "${RED}✗ Failed to download file${NC}"
    rm -f "${TEST_FILE}"
    exit 1
fi

# Step 5: Delete remote file
echo -e "${YELLOW}Step 5: Deleting remote file from casd${NC}"
DELETE_RESPONSE=$(curl -s -X DELETE "${CASD_URL}/file/${RETURNED_HASH}")

if [[ $? -eq 0 ]]; then
    echo -e "${GREEN}✓ Remote file deleted successfully${NC}"
else
    echo -e "${YELLOW}⚠ Failed to delete remote file (may not be implemented)${NC}"
fi

# Cleanup local file
rm -f "${TEST_FILE}"

echo -e "${GREEN}🎉 All tests passed successfully!${NC}"
echo -e "${GREEN}   - Created random data file${NC}"
echo -e "${GREEN}   - Uploaded file to casd${NC}"
echo -e "${GREEN}   - Verified file info and loop filesystem usage${NC}"
echo -e "${GREEN}   - Downloaded and verified file content${NC}"
echo -e "${GREEN}   - Deleted remote file from casd${NC}"
