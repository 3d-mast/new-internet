plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "net.newinternet.boreal"
    compileSdk = 35
    defaultConfig {
        applicationId = "net.newinternet.boreal"
        minSdk = 26
        targetSdk = 35
        versionCode = 60000
        versionName = "6.0.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.15.0")
    testImplementation("junit:junit:4.13.2")
}
