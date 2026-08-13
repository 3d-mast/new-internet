package net.newinternet.boreal

import java.nio.ByteBuffer
import java.nio.ByteOrder
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

object BorealFrame {
    const val MAGIC = 0x424f5231
    const val VERSION: Byte = 1
    const val HEADER = 20
    const val TAG = 16
    const val MAX_PAYLOAD = 1200

    fun seal(key: ByteArray, type: Byte, flags: Short, session: Int, sequence: Long, payload: ByteArray): ByteArray {
        require(key.size == 32)
        require(payload.size <= MAX_PAYLOAD)
        val header = ByteBuffer.allocate(HEADER).order(ByteOrder.BIG_ENDIAN)
            .putInt(MAGIC).put(VERSION).put(type).putShort(flags).putInt(session).putLong(sequence).array()
        val nonce = ByteBuffer.allocate(12).order(ByteOrder.BIG_ENDIAN).putInt(session).putLong(sequence).array()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(header)
        return header + cipher.doFinal(payload)
    }

    fun open(key: ByteArray, frame: ByteArray): ByteArray {
        require(key.size == 32)
        require(frame.size >= HEADER + TAG)
        val h = ByteBuffer.wrap(frame, 0, HEADER).order(ByteOrder.BIG_ENDIAN)
        require(h.int == MAGIC)
        require(h.get() == VERSION)
        h.get(); h.short
        val session = h.int
        val sequence = h.long
        require(frame.size - HEADER - TAG <= MAX_PAYLOAD)
        val nonce = ByteBuffer.allocate(12).order(ByteOrder.BIG_ENDIAN).putInt(session).putLong(sequence).array()
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(128, nonce))
        cipher.updateAAD(frame.copyOfRange(0, HEADER))
        return cipher.doFinal(frame.copyOfRange(HEADER, frame.size))
    }
}
