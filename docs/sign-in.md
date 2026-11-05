# Sign-in on the site: the owner's setup

The site signs people in with Telegram or Google through maply's own server
(`internal/account`). Both providers need a client registered by the owner of
@maplyin_bot and of a Google Cloud project. Until a provider's two keys are
in `.env`, its button on the site is greyed out and the rest of the site
works as before. The product side is in
[`design.md`](design.md), under "Accounts on the site".

The addresses below are for production, `APP_URL=https://maply.ws04.cc`. A
test bot on another address registers its own.

## 1. Telegram: BotFather

1. Open @BotFather's Mini App (the Open button in its chat), pick
   @maplyin_bot, then Login Widget.
2. Under Allowed URLs add the site and its callback, exactly:
   - `https://maply.ws04.cc`
   - `https://maply.ws04.cc/auth/telegram/callback`

   Telegram sends the browser back only to an address on this list.
3. Under Advanced keep the signing algorithm at **RS256**. maply accepts RS256
   only.
4. Copy the Client ID, which is the bot's numeric id, and the Client Secret.

maply asks Telegram for `openid profile`: the id, the name and the username.
It does not ask for the phone number or for permission to write to the
person (`telegram:bot_access`); the site has a button to the bot for the
daily post instead.

## 2. Google: Google Cloud

In the [Google Cloud console](https://console.cloud.google.com/), create a
project named `maply` and open Google Auth Platform.

1. **Branding**
   - App name: `maply`
   - User support email: the owner's address
   - App home page: `https://maply.ws04.cc/`
   - Privacy policy: `https://maply.ws04.cc/privacy`
   - Authorized domain: `ws04.cc`
   - A logo is optional. Adding one starts a review, so add it together with
     brand verification below.
2. **Audience**: User type **External**, then **Publish app**. With `openid`
   alone there is no scope review and no 100-user limit, which apply to
   sensitive and restricted scopes only.
3. **Data access**: add no scopes. maply asks for `openid` only, so Google
   gives it a permanent account number (`sub`) and no address, name or photo.
4. **Clients**: Create client, type **Web application**.
   - Authorized redirect URI: `https://maply.ws04.cc/auth/google/callback`
   - Authorized JavaScript origins: none. The sign-in runs on the server.
   - Copy the Client ID and the Client Secret.

## 3. Google: brand verification

Without it, Google's sign-in screen names the domain and not the app. It is
optional.

1. In [Search Console](https://search.google.com/search-console) add the
   domain property `ws04.cc`, signed in as the account that owns the Cloud
   project.
2. Search Console gives a TXT record. Add it in Cloudflare, in the DNS of
   `ws04.cc`, and press Verify.
3. Back in Google Auth Platform, Branding, submit the brand for verification.
   It needs the public home page and the privacy page above, both on
   `ws04.cc`.

## 4. The keys

Add the four keys to the `.env` next to the production compose file on ws04,
where `TELEGRAM_BOT_TOKEN` is:

```sh
TELEGRAM_LOGIN_CLIENT_ID=
TELEGRAM_LOGIN_CLIENT_SECRET=
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
```

Each pair goes together, and both need `APP_URL` to be the address
registered above. The service refuses to start with half a pair, and logs
`site sign-in` with which providers are on, never the keys. The secrets stay
in `.env` and nowhere else; `.env.example` has the names only.

## 5. Check it

1. Open `https://maply.ws04.cc/`, press Sign in, then Telegram. The browser
   comes back signed in, and the menu under the avatar shows the name.
2. On the accounts page, link Google. Sign out, then sign in with Google: it
   is the same account.
3. In the Mini App, open the days, then Accounts, then link Google. The phone's
   browser opens and names the Telegram account before going to Google.
4. Answer today on the site while signed in with Telegram. The bot's Mini App
   shows the same answer and refuses a second.

Before the first deploy with sign-in, apply core migration 021
(`core.touch_person`): a Telegram sign-in records the person with it, without
a chat, so nobody is put on the morning post by signing in.
