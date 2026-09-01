---
title: Incident report
date: Thursday 25 July 2025
type: Phishing
author: C. PETIT
reference: IR-COMPANY-002
client: COMPANY
victim: A. MARTIN
email: alice.martin@example.com
---

# Summary

On 23 July at 14:37, a phishing email titled "{{client}} - {{client}}" was sent from
the account of {{victim}} ([{{email}}](mailto:{{email}})).
It reached **190 internal recipients** and **19 external recipients**.

Analysis of the sign-in logs identified several people who interacted with the
malicious link.

# Description

On 21 July at 16:34, a sign-in from Milan, Italy was observed on the account of
{{victim}}, followed by several sign-ins from unusual IP addresses.

![Suspicious sign-ins on the account of {{victim}}](images/connections.png)

The attacker then added their own phone to the authentication methods available for
completing MFA.

![MFA changed on the account of {{victim}}](images/mfa.png){width=70%}

The link in the email redirected to `https://hearingplus.com.au/edocument/`, then to a
fake Microsoft sign-in page hosted on a subdomain of `manatoon76.com`.

## Indicators of compromise

| Type | Indicator | Action |
| --- | --- | --- |
| Domain | hearingplus.com.au | Blocked |
| Domain | *.manatoon76.com | Blocked |
| IP address | 93.184.216.34 | Monitored |

# Actions taken

The incident was reported on Wednesday 23 July at 15:57 and picked up at 16:20 by the
response team:

- M. MEZIRARD (Analyst)
- A. MORIN (Supervisor)

The following measures were applied, in order:

1. Password reset on the compromised account.
2. Active sessions and the added MFA methods revoked.
3. Malicious domains blocked at the Microsoft 365 tenant level.

The following accounts were identified as having interacted with the link:

- {{victim}} ({{email}})
- B. LEROY (bruno.leroy@example.com)
- C. FONTAINE (claire.fontaine@example.com)

# Impact

- **Loss of trust**: the contacts of {{victim}} may lose confidence, having received
  phishing emails from that account.
- **Risk of disclosure**: any recipient who entered credentials on the fake page should
  have their account treated as compromised.
- **Spread of the threat**: the emails sent may be forwarded to other organisations.

# Recommendations

1. Tighten the conditional access policy so that Microsoft 365 is reachable only from
   devices the organisation manages.
2. Deploy a cyber-awareness training platform.
3. Improve logging so that only relevant alerts reach the SOC.

> The recommendations above are listed in decreasing order of priority.
