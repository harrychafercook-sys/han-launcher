# Security & Antivirus False Positives Report

## Overview
This document addresses the remaining "False Positive" detections that may appear when scanning the **DayZ Launcher** executable.

**Status:** The application is **Safe and Digitally Signed**.
These detections are "heuristics" (guesses) triggered by the AI engines of certain antivirus vendors, specifically affecting the new code signing certificate until it builds reputation.

## Summary of Detections
You may see flags from the following vendors:
- **Avast:** `Win64:Evo-gen [Trj]`
- **AVG:** `Win64:Evo-gen [Trj]`

### Why this happens
1.  **Generic Heuristics (`Evo-gen`):** The tag `Evo-gen` stands for "Evolutionary Generation". It is a generic catch-all for files that the antivirus does not recognize.
2.  **New Certificate:** We have recently switched to a new EV Code Signing Certificate ("Open Source Developer, Harry Benjamin Chafer-Cook"). Security vendors often flag binaries signed by new, low-reputation certificates as a precaution until a sufficient number of users have downloaded them safely.
3.  **Go & Wails Architecture:** The application is built using **Go**, which produces large statically linked binaries. This structure, combined with a new certificate, can trigger aggressive heuristic filters.

## Verification
**[View Latest VirusTotal Scan Report](https://www.virustotal.com/gui/file/574b28e1e650f3097ecbff36efde3de3d72f857eb41435d0e4c6ffd5e82300bb?nocache=1)**

Since implementing Code Signing, the application is now **Undetected** (Clean) by the vast majority of industry leaders, including:
- **Microsoft (Defender)** - Clean
- **CrowdStrike** - Clean
- **SentinelOne** - Clean
- **Kaspersky** - Clean
- **Malwarebytes** - Clean
- **BitDefender** - Clean
- **Sophos** - Clean

## Actions Taken
We have submitted the false positive samples to Avast and AVG for manual analysis. We expect these detections to be cleared as our certificate gains reputation.

## Conclusion
If Avast or AVG flags the launcher, it is a confirmed false positive.
You can verify the digital signature on the `.exe` yourself:
- **Signer:** `Open Source Developer, Harry Benjamin Chafer-Cook`
- **Status:** `Valid`
