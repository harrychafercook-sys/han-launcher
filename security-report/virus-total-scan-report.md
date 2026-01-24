# Security & Antivirus False Positives Report

## Overview
This document addresses the remaining "False Positive" detections that may appear when scanning the **DayZ Launcher** executable.

**Status:** The application is **Safe and Digitally Signed**.
These detections are "heuristics" (guesses) triggered by the AI engines of certain antivirus vendors, specifically affecting the new code signing certificate until it builds reputation.

## Summary of Detections
You may see a flag from the following vendor:
- **Bkav Pro:** `W64.AIDetectMalware`

### Why this happens
1.  **AI Detection False Positive:** Bkav Pro uses an AI-based antivirus engine that often flags legitimate, newly signed software as malicious ("AIDetectMalware") simply because it hasn't seen the file before. This engine has a known high rate of false positives.
2.  **New Certificate:** We have recently switched to a new EV Code Signing Certificate ("Open Source Developer, Harry Benjamin Chafer-Cook"). Security vendors often flag binaries signed by new, low-reputation certificates as a precaution until a sufficient number of users have downloaded them safely.
3.  **Go & Wails Architecture:** The application is built using **Go**, which produces large statically linked binaries. This structure, combined with a new certificate, can trigger aggressive heuristic filters.

## Verification
**[View Latest VirusTotal Scan Report](https://www.virustotal.com/gui/file/1fc19d34f2db4ea2b91c922786a06a13a7ba874e276d546ddf334972bdb80f2f?nocache=1)**

Since implementing Code Signing, the application is now **Undetected** (Clean) by the vast majority of industry leaders, including:
- **Microsoft (Defender)** - Clean
- **CrowdStrike** - Clean
- **SentinelOne** - Clean
- **Kaspersky** - Clean
- **Malwarebytes** - Clean
- **BitDefender** - Clean
- **Sophos** - Clean

## Actions Taken
We are monitoring this detection. Given that it is a single engine known for false positives, it is expected to clear as the certificate gains reputation.

## Conclusion
If Bkav Pro flags the launcher, it is a confirmed false positive.
You can verify the digital signature on the `.exe` yourself:
- **Signer:** `Open Source Developer, Harry Benjamin Chafer-Cook`
- **Status:** `Valid`
