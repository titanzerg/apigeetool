# OpenAPI → Apigee ProxyEndpoint Converter

เครื่องมือ CLI ขนาดเล็ก (ภาษา Go) สำหรับเปลี่ยนไฟล์ OpenAPI 3.0 (`openapi.yaml`) ให้กลายเป็นไฟล์ ProxyEndpoint XML ที่ Apigee นำไปใช้ได้ทันที เหมาะสำหรับลดงานทำมือและคุม version ของ proxy configuration ใน git

## คุณสมบัติเด่น

- อ่านเฉพาะส่วน `info` และ `paths` ของ OpenAPI ทำให้ไม่ต้องแก้ spec เดิม
- สร้าง `<Flow>` ให้ครบทุก path + method พร้อม condition `(proxy.pathsuffix MatchesPath …)`
- กำหนดค่า `<ProxyEndpoint name>`, `<BasePath>` และไฟล์ output เองได้
- รักษา order ของ path/method ตามที่เขียนในไฟล์ YAML เพื่อให้ diff อ่านง่าย
- สามารถดึง ProxyEndpoint XML จาก Apigee (ผ่าน Management API) ลงมาเก็บในโฟลเดอร์ท้องถิ่นได้เลย พร้อมคำนวณ similarity/รายการ flow ที่ต่างกัน และถามยืนยันก่อนจะ update ไฟล์ปลายทางอัตโนมัติ
- มีโหมด `-findproxy` สำหรับค้นหา proxy ที่มี BasePath ตรงกับที่กำหนดโดยไม่ยุ่งกับไฟล์ OpenAPI

## โครงสร้างสำคัญใน repo

- `main.go` – โค้ดหลักที่แปลง YAML → XML
- `openapi.yaml` – ตัวอย่างสเปคที่ใช้เป็น input
- `proxy-endpoint.xml` / `newproxy.xml` – ตัวอย่างไฟล์ผลลัพธ์สำหรับเทียบโครงสร้าง
- `apigee_ts_map.sh` และไฟล์ `*.xml`, `*.txt` อื่นๆ – ตัวอย่าง configuration ที่เกี่ยวข้องกับโปรเจ็กต์จริง

## สิ่งที่ต้องมี (Requirements)

- Go 1.20 ขึ้นไป (ทดสอบบน 1.21) พร้อม `go` CLI
- ไฟล์ OpenAPI 3.0 ในรูปแบบ YAML (ค่าเริ่มต้นคือ `openapi.yaml` ในรากโปรเจ็กต์)

## วิธีเริ่มต้นใช้งาน

1. ติดตั้ง dependency (ถ้าเพิ่ง clone มา)
   ```bash
   go mod tidy
   ```
2. เตรียมไฟล์ `openapi.yaml` ให้มีข้อมูล `info.title` และ `paths` ครบ
3. คัดลอกไฟล์ `.env.example` ไปเป็น `.env` แล้วใส่ค่า `APIGEE_ORG`, `APIGEE_TOKEN`, ฯลฯ (CLI จะอ่านอัตโนมัติถ้าไฟล์อยู่ในโฟลเดอร์เดียวกัน)
4. สั่งรันคำสั่งด้านล่างเพื่อสร้าง ProxyEndpoint XML

```bash
go run . \
  -input openapi.yaml \
  -output proxy-endpoint.xml \
  -name default \
  -basepath /scm-micro-service-psb-dashboard
```

### อธิบาย flag

- `-input` : path ของไฟล์ OpenAPI (default `openapi.yaml`)
- `-output` : path ของไฟล์ XML ที่ต้องการให้สร้าง (default `proxy-endpoint.xml`)
- `-name` : ใส่ชื่อ ProxyEndpoint ใน `<ProxyEndpoint name="...">` (default `default`)
- `-basepath` : ค่า `<HTTPProxyConnection><BasePath>` (ถ้าไม่ใส่ จะ slugify จาก `info.title` แล้วเติม `/` ให้อัตโนมัติ)

เมื่อรันสำเร็จ CLI จะขึ้นข้อความ `Generated <จำนวน flows> flows at <path>`. เราสามารถเปิดไฟล์ปลายทางไปเทียบกับ `newproxy.xml` ได้เพื่อเช็กว่าโครงสร้างถูกต้องตามที่ต้องการ

### Flag สำหรับเชื่อมต่อ Apigee (ดาวน์โหลด ProxyEndpoint)

| Flag | คำอธิบาย |
| --- | --- |
| `-proxy` / `-p` | ชื่อ API Proxy บน Apigee ที่ต้องการดาวน์โหลด ProxyEndpoint ทั้งหมด |
| `-org` | ชื่อ organization (ถ้าไม่ใส่ จะอ่านจาก env `APIGEE_ORG`) |
| `-token` | Bearer token เพื่อเรียก Management API (ถ้าไม่ใส่ จะอ่านจาก env `APIGEE_TOKEN`) |
| `-revision` | ระบุ revision เฉพาะ (ถ้าไม่ใส่จะไปดึง revision ล่าสุดให้อัตโนมัติ) |
| `-apigee-host` | base URL ของ Apigee Management API (default `https://apigee.googleapis.com`) |
| `-download-dir` | โฟลเดอร์ปลายทางที่ต้องการเซฟไฟล์ XML (default `downloaded-proxy-endpoints` และจะลบไฟล์เดิมทุกครั้งก่อนดาวน์โหลดใหม่) |
| `-findproxy` / `-f` | ใส่ BasePath เพื่อค้นหา proxy ที่ใช้งาน BasePath นั้น (ค้นหาแบบ contains, ไม่สร้างไฟล์ใหม่) |
| `-compare` / `-c` | เปรียบเทียบ proxy สอง revision (ต้องระบุ `-proxy` และตัวเลข revision 2 ค่า โดยใส่ revision เก่าก่อนใหม่) |
| `-db-url` | ใส่ PostgreSQL connection URL เพื่อใช้ทั้ง `-sync` และ `-findproxy` (default อ่านจาก `APIGEE_SYNC_DB_URL` หรือ `DATABASE_URL`) |
| `-db-ssl-rootcert` / `-db-ssl-cert` / `-db-ssl-key` | ตั้งค่าไฟล์ TLS สำหรับเชื่อมต่อ DB (ใช้ร่วมทั้ง `-sync` และ `-findproxy`) default อ่านจากตัวแปร `APIGEE_SYNC_DB_SSL_*` |
| `-endpoints-table` | ชื่อตาราง proxy endpoints ใน Postgres ใช้ร่วมทั้ง `-sync` และ `-findproxy` (default `apigee.apigee_proxy_endpoints`) |
| `-target-endpoints-table` | ชื่อตาราง target endpoint details ใน Postgres สำหรับโหมด `-sync` (default `apigee.apigee_target_endpoints`) |
| `-targets-table` | ชื่อตาราง target servers ใน Postgres สำหรับโหมด `-sync` (default `apigee.apigee_target_servers`) |
| `-products-table` | ชื่อตาราง API products ใน Postgres สำหรับโหมด `-sync` (default `apigee.apigee_api_products`) |
| `-apps-table` | ชื่อตาราง apps ใน Postgres สำหรับโหมด `-sync` (default `apigee.apigee_apps`) |
| `-app-credentials-table` | ชื่อตาราง app credentials ใน Postgres สำหรับโหมด `-sync` (default `apigee.apigee_app_credentials`) |
| `-proxy-flows-table` | ชื่อตาราง pre/post flow steps ของ proxy endpoint ใน Postgres สำหรับโหมด `-sync` (default `apigee.apigee_proxy_endpoint_flows`) |
| `-sync` | เปิดโหมดซิงก์ลง PostgreSQL (ไม่สร้างไฟล์ XML) เลือกได้ `all` (default เมื่อใส่ flag), `apiproxy`, `target_server`, `api_product`, `apps` หรือคอมมาแยกหลายค่า |

### โหมดซิงก์ข้อมูล proxy endpoints → PostgreSQL

ตั้งค่า `APIGEE_ORG`/`APIGEE_TOKEN` ตามปกติ จากนั้นระบุ connection string ของ Postgres ด้วย flag `-db-url` หรือ environment variables ข้างต้น แล้วสั่ง

```bash
go run . -sync -org my-org -token "$APIGEE_TOKEN" -db-url postgres://...
```

ไม่ระบุค่าถัดจาก `-sync` จะซิงก์ทั้ง proxy endpoints, target server, API product และ apps (เทียบเท่า `-sync=all`). ถ้าต้องการเฉพาะบางอย่างให้ระบุชื่อตามนี้ เช่น `-sync=apiproxy`, `-sync=target_server`, `-sync=apps` หรือรวมหลายรายการแบบ `-sync=apiproxy,api_product`

เมื่อซิงก์ทั้งหมด (`-sync` หรือ `-sync=all`) คำสั่งจะ

1. ไล่เรียก proxy ทุกตัวในองค์กร
2. ดึง revision ล่าสุดและอ่านค่า BasePath พร้อม TargetEndpoint servers, TargetEndpoint details (URL/LoadBalancer/Properties), จำนวน flows, และ environment ที่ deploy ของ ProxyEndpoint ทุกไฟล์
3. ล้างแถวทั้งหมดในตารางเป้าหมาย
4. ใส่ข้อมูลล่าสุด (proxy, endpoint, revision, base path, target servers, environments, flow count, updated_at) กลับลงไปใหม่ภายในการทำธุรกรรมเดียว
5. ดึงข้อมูล target server ที่อ้างอิงในแต่ละ environment แล้วบันทึกลง table เป้าหมายสำหรับ target server แยกต่างหาก
6. ดึงข้อมูล API product ทั้งหมดแล้วบันทึกลงตาราง API products
7. ดึงข้อมูล apps พร้อม credentials แล้วบันทึกลงตาราง apps + app credentials
8. ดึงข้อมูล TargetEndpoint details แล้วบันทึกลงตาราง target endpoints
9. ดึงข้อมูล pre/post flow steps ของ ProxyEndpoint แล้วบันทึกลงตาราง proxy endpoint flows

> ระบบจะบังคับ `sslmode=require` อัตโนมัติ และจะเติมค่า `sslrootcert`, `sslcert`, `sslkey` จาก flag/ตัวแปรสภาพแวดล้อมที่ตั้งไว้ เพื่อให้เชื่อมต่อผ่าน TLS โดยใช้ไฟล์ `.pem` ที่คุณเตรียมไว้

สำหรับการตั้งค่าผ่าน `.env` ให้ระบุ

```dotenv
APIGEE_SYNC_DB_URL=postgres://.../mydb?sslmode=require
APIGEE_SYNC_DB_SSL_ROOTCERT=certs/server-ca.pem
APIGEE_SYNC_DB_SSL_CERT=certs/client-cert.pem
APIGEE_SYNC_DB_SSL_KEY=certs/client-key.pem
```

ไฟล์ `.pem` ทั้งสามต้องอยู่บนเครื่องเดียวกับ CLI และระบุ path ที่เข้าถึงได้จริง (สามารถเป็น absolute หรือ relative path ก็ได้)

ตัวอย่าง schema ที่ใช้ร่วมกันได้

```sql
CREATE SCHEMA IF NOT EXISTS apigee;

CREATE TABLE apigee.apigee_proxy_endpoints (
  proxy_name text NOT NULL,
  endpoint_name text NOT NULL,
  revision integer NOT NULL,
  base_path text NOT NULL,
  target_servers text[] NOT NULL DEFAULT '{}',
  environments text[] NOT NULL DEFAULT '{}',
  flow_count integer NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (proxy_name, endpoint_name)
);

CREATE TABLE apigee.apigee_target_endpoints (
  proxy_name text NOT NULL,
  endpoint_name text NOT NULL,
  target_endpoint_name text NOT NULL,
  target_url text NULL,
  load_balancer_servers text[] NOT NULL DEFAULT '{}',
  properties jsonb NOT NULL DEFAULT '{}'::jsonb,
  success_codes text NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (proxy_name, endpoint_name, target_endpoint_name)
);

CREATE TABLE apigee.apigee_proxy_endpoint_flows (
  proxy_name text NOT NULL,
  endpoint_name text NOT NULL,
  preflow_request_steps text[] NOT NULL DEFAULT '{}',
  preflow_response_steps text[] NOT NULL DEFAULT '{}',
  postflow_request_steps text[] NOT NULL DEFAULT '{}',
  postflow_response_steps text[] NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (proxy_name, endpoint_name)
);

CREATE TABLE apigee.apigee_target_servers (
  name text NOT NULL,
  environment text NOT NULL,
  url text NOT NULL,
  host text NOT NULL,
  port integer NOT NULL,
  is_ssl boolean NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (environment, name)
);

CREATE TABLE apigee.apigee_api_products (
  name text NOT NULL,
  environments text[] NOT NULL DEFAULT '{}',
  apiproxies text[] NOT NULL DEFAULT '{}',
  apps text[] NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (name)
);

CREATE TABLE apigee.apigee_apps (
  app_id text NOT NULL,
  name text NOT NULL,
  owner text NULL,
  registered_at timestamptz NULL,
  notes text NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (app_id)
);

CREATE TABLE apigee.apigee_app_credentials (
  app_id text NOT NULL,
  consumer_key text NOT NULL,
  consumer_secret text NOT NULL,
  expires_at timestamptz NULL,
  products text[] NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (app_id, consumer_key)
);
```

ตัวอย่าง query หา proxy ที่ยังไม่ได้ตั้งค่า `success.codes` ใน `<HTTPTargetConnection><Properties>`

```sql
SELECT proxy_name, endpoint_name, target_endpoint_name
FROM apigee.apigee_target_endpoints
WHERE success_codes IS NULL OR success_codes = '';
```

ต้องให้สิทธิ์บัญชีที่ใช้เชื่อมต่อสามารถ `DELETE` + `INSERT` กับตารางดังกล่าว

## ตัวอย่างการใช้งาน

สร้างไฟล์ใหม่ชื่อ `newproxy.xml` จาก spec เริ่มต้น:

```bash
go run . -input openapi.yaml -output newproxy.xml
```

ถ้าอยากทดสอบเฉพาะบาง path ให้ลบ path อื่นออกจาก `openapi.yaml` แล้วรันคำสั่งเดิมอีกครั้ง

ดาวน์โหลด ProxyEndpoint XML ทุกไฟล์ของ proxy `my-proxy` (อิง revision ล่าสุด) — ถ้ามี `.env` แล้วสามารถเริ่มต้นตรง `go run` ได้เลย:

```bash
export APIGEE_ORG=my-org
export APIGEE_TOKEN="$(gcloud auth print-access-token)"
go run . -proxy my-proxy -download-dir apigee-proxy-endpoints
```

คำสั่งจะดึง zip bundle ของ proxy revision ที่ใหม่ที่สุด, ลบไฟล์เก่าทิ้งก่อน แล้วแตกเฉพาะไฟล์ใน `apiproxy/proxy-endpoints/` ลงในโฟลเดอร์ที่กำหนด

ค้นหา proxy ที่มี BasePath ตรงกับ `/marine-dashboard` (มี `.env` แล้วก็เริ่มที่ `go run` ได้เหมือนกัน)

```bash
export APIGEE_ORG=my-org
export APIGEE_TOKEN="$(gcloud auth print-access-token)"
go run . -findproxy /marine-dashboard
```

สามารถใส่เพียงบางส่วนของ path เช่น `-findproxy material` เพื่อให้ค้นหาแบบ contains (case-insensitive)

ระหว่างค้นหา CLI จะแสดง progress ของแต่ละ proxy ในรูปแบบ `[3/57] my-proxy (rev 12) basepaths: /foo, /bar` ถ้า base path มี substring ตรงกับที่ระบุจะมีบรรทัด `-> MATCH` ต่อท้าย

ถ้าซิงก์ข้อมูล proxy endpoints ลง Postgres ไว้แล้ว สามารถค้นหาจาก cache แทนการดาวน์โหลดทีละ proxy ได้ด้วย

```bash
go run . -findproxy /marine-dashboard -db-url "$APIGEE_SYNC_DB_URL"
```

CLI จะดึงผลจากตาราง `apigee.apigee_proxy_endpoints` (หรือชื่อตารางที่ระบุ) โดยตรง ทำให้ได้ผลลัพธ์เร็วขึ้นและไม่ต้องใช้ token Apigee

หลังดาวน์โหลดเสร็จ CLI จะคำนวณ similarity ระหว่างไฟล์ที่ generate (`proxy-endpoint.xml`) กับไฟล์ในโฟลเดอร์ดาวน์โหลด แล้วพิมพ์ชื่อไฟล์ที่ใกล้เคียงที่สุดพร้อมเปอร์เซ็นต์ พร้อมสรุปส่วนต่างของ flow (flow ที่เพิ่ม/หาย และ condition/description ที่ไม่ตรงกัน) เพื่อใช้ตรวจโครงสร้างได้เร็วขึ้น จากนั้นจะถามว่ายืนยันให้อัปเดตไฟล์ดาวน์โหลดให้ตรงกับไฟล์ที่ generate หรือไม่ (ตอบ `y` เพื่อเขียนทับ, กด Enter/`n` เพื่อข้าม)

### โหมดเปรียบเทียบสอง revision ของ proxy (`-compare`)

ใช้สำหรับเปรียบเทียบไฟล์ ProxyEndpoint/TargetEndpoint/Policies/Resources ระหว่างสอง revision ของ proxy เดียวกัน โดยต้องระบุชื่อ proxy และตัวเลข revision 2 ค่า (ใส่ revision เก่าก่อนใหม่)

ตัวอย่าง (alias `-c` ใช้งานได้เหมือนกัน):

```bash
export APIGEE_ORG=my-org
export APIGEE_TOKEN="$(gcloud auth print-access-token)"
go run . -compare -proxy my-proxy 12 11
```

สิ่งที่คำสั่งทำ:

1. ดาวน์โหลด bundle ของ revision 11 และ 12 ลงโฟลเดอร์ `downloaded-proxy-endpoints/compare-rev-11` และ `downloaded-proxy-endpoints/compare-rev-12`
2. เปรียบเทียบไฟล์ ProxyEndpoint (รวม BasePath และ Flow diff), TargetEndpoint, Policies และ Resources
3. สรุปความต่างด้วยข้อความ เช่น “only in …” หรือ “differs” หากมีเครื่องมือ `diff` จะพิมพ์ unified diff ให้ด้วย (รวมไฟล์ Policies/Resources)

หมายเหตุ:
- ต้องมีสิทธิ์อ่าน proxy bundle และมี token ใช้งานได้
- ถ้าไม่พบความต่าง ระบบจะแสดง “No differences detected.”
- แสดง unified diff เฉพาะบรรทัดที่เปลี่ยน (ไม่มี context) และไฟล์ XML ที่ต่างเฉพาะ indent จะไม่ถือว่าต่าง
- รายการ “Flows only in …” จะแสดงด้วยสัญลักษณ์ `+`/`-` เพื่อให้ตรงกับความหมายของ diff

## เคล็ดลับ & การดีบัก

- หากเรียก `-proxy` แล้วขึ้น error ว่า “token required” ให้ตรวจสอบ env `APIGEE_TOKEN` หรือส่ง `-token` เป็น Bearer token เอง (สามารถใช้ `gcloud auth print-access-token` ได้ถ้าเป็น Apigee X ที่เชื่อมกับ Google Cloud)
- สามารถตั้งค่า `APIGEE_ORG`, `APIGEE_TOKEN`, `APIGEE_SYNC_DB_URL`, ฯลฯ ไว้ในไฟล์ `.env` เพื่อไม่ต้อง `export` ทุกครั้ง (มีตัวอย่างอยู่ใน `.env.example`)
- หากเจอ error 403/404 ตอนดาวน์โหลด ให้เช็กว่า proxy name, org, และสิทธิ์ของ token ถูกต้อง รวมถึงตรวจสอบว่า proxy นั้นมี revision แล้วจริงๆ
- หากไม่ได้ไฟล์ ProxyEndpoint เลย ให้ตรวจว่า proxy มีไฟล์อยู่ใน folder `apiproxy/proxy-endpoints` หรือ `apiproxy/proxies` (เครื่องมือรองรับทั้งสองโครงสร้าง)
- โหมด `-findproxy` จะดาวน์โหลด bundle ของแต่ละ proxy มาตรวจ BasePath ภายในไฟล์ endpoint เพราะฉะนั้นต้องเตรียม token ที่มีสิทธิอ่าน proxy bundle
- ระหว่าง generate CLI จะตรวจ naming ของ `paths` ใน OpenAPI และพิมพ์ warning พร้อมตัวอย่างแก้ (ไม่ block การสร้างไฟล์)
- หาก CLI แจ้งว่า “no paths defined” ให้ตรวจสอบว่าไฟล์ YAML มีส่วน `paths:` และจัด indent ถูกต้อง
- ถ้าต้องการรักษา order ของ path/method ให้แก้ไฟล์ `openapi.yaml` ด้วยเครื่องมือที่ไม่สลับลำดับ key (เช่น VS Code + YAML extension)
- คำอธิบายของแต่ละ flow จะใช้ `summary` ถ้ามี, ถ้าไม่มีจะถอยไปใช้ `description` หรือสร้างจาก `<METHOD> <Path>` ให้อัตโนมัติ

## การปรับแต่งเพิ่มเติม

- สามารถนำ XML ที่สร้างไปใส่ในโฟลเดอร์ proxy ของ Apigee แล้ว deploy ได้ทันที
- หากต้องการ build เป็น binary:
  ```bash
  go build -o openapi2apigee
  ./openapi2apigee -input openapi.yaml -output proxy-endpoint.xml
  ```

หากพบปัญหาเพิ่มเติมให้เปิด issue หรือแนบ spec ที่ใช้จริงเพื่อจะได้ช่วยตรวจสอบโครงสร้างให้ได้ง่ายขึ้นครับ 😊
