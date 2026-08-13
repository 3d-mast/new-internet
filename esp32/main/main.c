#include <stdint.h>
#include <string.h>
#include "esp_log.h"
#include "mbedtls/gcm.h"

#define BOREAL_MAGIC 0x424f5231u
#define BOREAL_VERSION 1u
#define BOREAL_MAX_PAYLOAD 1200u

static const char *TAG = "boreal";

static void be32(uint8_t *p, uint32_t v) {
    p[0]=(uint8_t)(v>>24); p[1]=(uint8_t)(v>>16); p[2]=(uint8_t)(v>>8); p[3]=(uint8_t)v;
}
static void be64(uint8_t *p, uint64_t v) {
    for (int i=7;i>=0;i--) { p[i]=(uint8_t)v; v >>= 8; }
}

static int boreal_seal(const uint8_t key[32], uint32_t session, uint64_t seq,
                       const uint8_t *plain, size_t len,
                       uint8_t *out, size_t out_cap, size_t *out_len) {
    if (len > BOREAL_MAX_PAYLOAD || out_cap < 20 + len + 16) return -1;
    be32(out, BOREAL_MAGIC); out[4]=BOREAL_VERSION; out[5]=1; out[6]=out[7]=0;
    be32(out+8, session); be64(out+12, seq);
    uint8_t nonce[12]; be32(nonce, session); be64(nonce+4, seq);
    mbedtls_gcm_context gcm; mbedtls_gcm_init(&gcm);
    int rc = mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key, 256);
    if (rc == 0) rc = mbedtls_gcm_crypt_and_tag(&gcm, MBEDTLS_GCM_ENCRYPT, len,
        nonce, sizeof(nonce), out, 20, plain, out+20, 16, out+20+len);
    mbedtls_gcm_free(&gcm);
    if (rc == 0) *out_len = 20 + len + 16;
    return rc;
}

void app_main(void) {
    static const uint8_t key[32] = {
        0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,
        0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42,0x42
    };
    const uint8_t hello[] = "boreal-esp32";
    uint8_t frame[20 + sizeof(hello) + 16]; size_t n = 0;
    int rc = boreal_seal(key, 1, 1, hello, sizeof(hello)-1, frame, sizeof(frame), &n);
    ESP_LOGI(TAG, "BOREAL/1 firmware self-test: rc=%d frame=%u bytes", rc, (unsigned)n);
}
