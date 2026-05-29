# THCoffee Guest WiFi

## Network overview

| Item | Value |
|------|-------|
| SSID | `THCoffee_Guest` |
| Band | 5 GHz (wlan2) |
| Security | Open (no password) |
| Guest subnet | `10.10.10.0/24` |
| Gateway / DNS | `10.10.10.1` |
| DHCP range | `10.10.10.10 – 10.10.10.250` |
| Idle timeout | 30 min |
| Hotspot server | `hotspot1` |
| Login page | Username only — no password field shown |
| Client isolation | Enabled — guests cannot reach each other |

## How guests connect

1. Connect to `THCoffee_Guest` (no password)
2. Open any HTTP URL — browser redirects to login page at `http://10.10.10.1`
3. Enter **username only** → click **Login** (no password field)
4. Internet access granted

> Guest devices are isolated from each other (wlan2 `default-forwarding=no`) and from the LAN (`192.168.88.0/24`).

---

## Logging

Guest hotspot traffic is captured by **dumet** running on `192.168.1.100`:
- Hotspot login/logout events logged to `hotspot_sessions` table (username, IP, MAC, duration)
- DHCP leases logged to `dhcp_leases` table (MAC → IP mapping)
- Firewall/NAT traffic logged to `traffic` table

All logs are stored in QuestDB with 180-day retention and tamper-evident hash-chain signing (Thai CCA compliance).

Web UI: `http://192.168.1.100:8088` (admin / see `/etc/dumet/dumet.env`)

---

## Managing accounts via MikroTik API

MikroTik API runs on port **8728** (plain) — accessible from `192.168.1.100` only (firewall-restricted).

### Install library

```bash
pip install routeros-api
```

### Connect

```python
import routeros_api

pool = routeros_api.RouterOsApiPool(
    host='192.168.1.39',   # MikroTik WAN IP (may change on DHCP rebind — check /ip address)
    username='admin',
    password='Qwerty123',
    port=8728,
    plaintext_login=True   # required for RouterOS 6.x
)
api = pool.get_api()
```

---

### List all hotspot users

```python
users = api.get_resource('/ip/hotspot/user')
for u in users.get():
    print(u)
```

---

### Create account

The login page shows **username only** — set `password=''` so guests just type their name.

```python
users = api.get_resource('/ip/hotspot/user')

users.add(
    name='john',
    password='',            # empty — login page has no password field
    server='hotspot1',
    comment='table 5'       # optional
)
```

**With data limits (optional):**

```python
users.add(
    name='john',
    password='',
    server='hotspot1',
    **{'limit-uptime': '8h'},          # kick after 8 hours total session time
    **{'limit-bytes-total': '1073741824'}  # kick after 1 GB
)
```

---

### Delete account

```python
users = api.get_resource('/ip/hotspot/user')

# find by name
result = users.get(name='john')
if result:
    users.remove(id=result[0]['id'])
```

---

### Disable account (keep but block login)

```python
users = api.get_resource('/ip/hotspot/user')

result = users.get(name='john')
if result:
    users.set(id=result[0]['id'], disabled='yes')
```

---

### Enable account

```python
users = api.get_resource('/ip/hotspot/user')

result = users.get(name='john')
if result:
    users.set(id=result[0]['id'], disabled='no')
```

---

### Kick active session (force logout)

```python
active = api.get_resource('/ip/hotspot/active')

sessions = active.get(user='john')
for s in sessions:
    active.remove(id=s['id'])
```

---

### Full helper script

```python
import routeros_api

MIKROTIK_HOST = '192.168.1.39'
MIKROTIK_USER = 'admin'
MIKROTIK_PASS = 'Qwerty123'
HOTSPOT_SERVER = 'hotspot1'


def get_api():
    pool = routeros_api.RouterOsApiPool(
        host=MIKROTIK_HOST,
        username=MIKROTIK_USER,
        password=MIKROTIK_PASS,
        port=8728,
        plaintext_login=True
    )
    return pool.get_api()


def list_users():
    api = get_api()
    return api.get_resource('/ip/hotspot/user').get()


def create_user(name, comment=''):
    """Password is empty — login page shows username only."""
    api = get_api()
    api.get_resource('/ip/hotspot/user').add(
        name=name,
        password='',
        server=HOTSPOT_SERVER,
        comment=comment
    )


def delete_user(name):
    api = get_api()
    res = api.get_resource('/ip/hotspot/user')
    users = res.get(name=name)
    for u in users:
        res.remove(id=u['id'])


def set_user_disabled(name, disabled=True):
    api = get_api()
    res = api.get_resource('/ip/hotspot/user')
    users = res.get(name=name)
    for u in users:
        res.set(id=u['id'], disabled='yes' if disabled else 'no')


def kick_user(name):
    api = get_api()
    active = api.get_resource('/ip/hotspot/active')
    sessions = active.get(user=name)
    for s in sessions:
        active.remove(id=s['id'])


# Example usage
if __name__ == '__main__':
    create_user('table1', comment='table 1')
    print(list_users())
    set_user_disabled('table1', disabled=True)
    set_user_disabled('table1', disabled=False)
    kick_user('table1')
    delete_user('table1')
```

---

## Notes

- MikroTik LAN/WAN IP (`192.168.1.39`) is assigned by DHCP — may change on rebind. Check current IP:
  ```bash
  ssh coffee@192.168.1.100 "sshpass -p 'Qwerty123' ssh admin@192.168.1.39 '/ip address print'"
  ```
- API port 8728 is plain text (binary RouterOS protocol). Firewall restricts it to `192.168.1.100` only. Never expose outside LAN.
- Login page (`flash/hotspot/login.html`) was modified: password field removed, `doLogin()` hashes empty string. If MikroTik resets to factory defaults, re-upload the modified file from `coffee@192.168.1.100:/tmp/login_modified.html`.
- Default `guest` account (no password) exists for walk-in use — disable it if switching to managed accounts:
  ```python
  set_user_disabled('guest', disabled=True)
  ```
- All hotspot logins are logged to QuestDB via dumet — username, MAC, IP, timestamps retained 180 days per Thai CCA requirements.
