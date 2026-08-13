package net.newinternet.boreal

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class BorealFrameTest {
    @Test fun roundTripAndTamper() {
        val key = ByteArray(32) { 0x42 }
        val plain = "android-boreal".encodeToByteArray()
        val frame = BorealFrame.seal(key, 2, 0, 7, 9, plain)
        assertArrayEquals(plain, BorealFrame.open(key, frame))
        frame[frame.lastIndex] = (frame.last().toInt() xor 1).toByte()
        var failed = false
        try { BorealFrame.open(key, frame) } catch (_: Exception) { failed = true }
        check(failed)
    }
}
