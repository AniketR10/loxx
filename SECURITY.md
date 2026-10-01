# Security

loxx saves your commands, so keeping secrets out of them matters most.

## Found a problem? Tell us privately

Go to the **Security** tab of this repository and click
**Report a vulnerability**. Please don't open a public issue.

Report it if:

- loxx saved a password, key or token instead of hiding it as `<REDACTED:…>`
- loxx sent anything over the internet
- loxx ran a command you didn't choose
- someone else on your computer could read your saved commands

## Never send us a real secret

Describe what it looked like, but replace the secret with a made-up one of the
same shape. For example:

```
export MYAPP_TOKEN=myapp_live_9f8e7d6c5b4a  (made up; real ones start with myapp_live_)
```

## If loxx saved a real secret

1. Delete it: `loxx forget --match 'part of the command'`
2. Change the secret (new password or new key), because it may have been
   exposed.

## Which versions get fixes

Only the newest release.
