package net.newinternet.boreal

import android.app.Activity
import android.os.Bundle
import android.widget.TextView

class MainActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(TextView(this).apply {
            text = "Boreal 6 · BOREAL/1\nAndroid protocol core ready"
            textSize = 20f
            setPadding(48, 64, 48, 48)
        })
    }
}
