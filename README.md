# OpenAPI → Apigee ProxyEndpoint Converter

เครื่องมือ CLI ขนาดเล็ก (ภาษา Go) สำหรับเปลี่ยนไฟล์ OpenAPI 3.0 (`openapi.yaml`) ให้กลายเป็นไฟล์ ProxyEndpoint XML ที่ Apigee นำไปใช้ได้ทันที เหมาะสำหรับลดงานทำมือและคุม version ของ proxy configuration ใน git

## คุณสมบัติเด่น

- อ่านเฉพาะส่วน `info` และ `paths` ของ OpenAPI ทำให้ไม่ต้องแก้ spec เดิม
- สร้าง `<Flow>` ให้ครบทุก path + method พร้อม condition `(proxy.pathsuffix MatchesPath …)`
- กำหนดค่า `<ProxyEndpoint name>`, `<BasePath>` และไฟล์ output เองได้
- รักษา order ของ path/method ตามที่เขียนในไฟล์ YAML เพื่อให้ diff อ่านง่าย

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
3. สั่งรันคำสั่งด้านล่างเพื่อสร้าง ProxyEndpoint XML

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

## ตัวอย่างการใช้งาน

สร้างไฟล์ใหม่ชื่อ `newproxy.xml` จาก spec เริ่มต้น:

```bash
go run . -input openapi.yaml -output newproxy.xml
```

ถ้าอยากทดสอบเฉพาะบาง path ให้ลบ path อื่นออกจาก `openapi.yaml` แล้วรันคำสั่งเดิมอีกครั้ง

## เคล็ดลับ & การดีบัก

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
