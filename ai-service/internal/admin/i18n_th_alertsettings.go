package admin

// Thai lines for the Alerts page (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"Alerts · Pronto": "การแจ้งเตือน · Pronto",
		"Alerts":          "การแจ้งเตือน",
		"How many times the bot answers one customer, and when the bot warns you that something is wrong. Changes apply at once.": "กำหนดว่าบอทตอบลูกค้าหนึ่งคนได้กี่ครั้ง และเมื่อไรบอทจะแจ้งเตือนคุณว่ามีอะไรผิดปกติ มีผลทันทีที่บันทึก",
		"Alert settings are off.":      "ปิดการตั้งค่าการแจ้งเตือนอยู่",
		"Reply limit for this account": "จำกัดจำนวนครั้งที่บอทตอบ (บัญชีนี้)",
		"Stops one customer who sends message after message from using up AI credit. When a customer reaches the limit, the bot stops answering them for the rest of the time window and passes the chat to your team once, with a private note. The customer gets no message.": "กันไม่ให้ลูกค้าคนเดียวที่ส่งข้อความรัว ๆ ใช้เครดิต AI จนหมด เมื่อลูกค้าถึงจำนวนที่กำหนด บอทจะหยุดตอบลูกค้าคนนั้นจนหมดช่วงเวลา และส่งแชทต่อให้ทีมงานหนึ่งครั้งพร้อมโน้ตส่วนตัว ลูกค้าจะไม่ได้รับข้อความใด ๆ",
		"Limit bot replies per customer": "จำกัดจำนวนครั้งที่บอทตอบลูกค้าแต่ละคน",
		"Replies per customer":           "จำนวนครั้งที่ตอบต่อลูกค้าหนึ่งคน",
		"Within":                         "ภายใน",
		"This account uses its own limit. The server default is %s.": "บัญชีนี้ใช้ค่าที่ตั้งเอง ค่าเริ่มต้นของเซิร์ฟเวอร์คือ %s",
		"This account uses the server default (%s) until you save.":  "บัญชีนี้ใช้ค่าเริ่มต้นของเซิร์ฟเวอร์ (%s) จนกว่าคุณจะบันทึก",
		"Bot alerts": "การแจ้งเตือนของบอท",
		"These apply to every account on this server. An alert goes to Slack once when a problem starts, again every 6 hours while it lasts, and once when it has been quiet for 15 minutes. It also shows on Home.": "ใช้กับทุกบัญชีบนเซิร์ฟเวอร์นี้ แจ้งเตือนจะส่งเข้า Slack หนึ่งครั้งเมื่อเริ่มมีปัญหา ส่งซ้ำทุก 6 ชั่วโมงระหว่างที่ปัญหายังอยู่ และส่งอีกครั้งเมื่อปกติต่อเนื่อง 15 นาที และแสดงที่หน้าแรกด้วย",
		"Slack: set":     "Slack: ตั้งค่าแล้ว",
		"Slack: not set": "Slack: ยังไม่ได้ตั้งค่า",
		"The Slack link is a server secret; it is not shown here.":                          "ลิงก์ Slack เป็นค่าลับของเซิร์ฟเวอร์ จึงไม่แสดงที่นี่",
		"Alerts show on Home and in the log only. Set %s in Dokploy to send them to Slack.": "ตอนนี้แจ้งเตือนแสดงเฉพาะที่หน้าแรกและใน log ตั้งค่า %s ใน Dokploy เพื่อส่งเข้า Slack",
		"Alert":     "แจ้งเตือน",
		"Sent when": "ส่งเมื่อ",
		"fixed":     "ค่าคงที่",
		"Set below": "ตั้งค่าด้านล่าง",
		"Counts customer messages the bot answered or handed to staff in the last hour. Chats where the customer asked for a person, the bot was off, or the reply limit was reached are not counted.": "นับข้อความลูกค้าที่บอทตอบหรือส่งต่อให้ทีมงานในหนึ่งชั่วโมงที่ผ่านมา ไม่นับแชทที่ลูกค้าขอคุยกับคน บอทปิดอยู่ หรือถึงจำนวนครั้งที่จำกัดแล้ว",
		"Share handed to staff (%)":            "สัดส่วนที่ส่งต่อให้ทีมงาน (%)",
		"At least this many answered messages": "จำนวนข้อความที่ตอบแล้วอย่างน้อย",
		"%d or more in %d minutes":             "ตั้งแต่ %d ครั้งใน %d นาที",
		"Any, at once":                         "ทันทีที่เกิด",
		"1 hour":                               "1 ชั่วโมง",
		"%d hours":                             "%d ชั่วโมง",
		"%d minutes":                           "%d นาที",
		"off":                                  "ปิด",
		"The saved alert settings could not be read, so the server defaults are in use. Saving this page replaces them.": "อ่านการตั้งค่าการแจ้งเตือนที่บันทึกไว้ไม่ได้ ตอนนี้จึงใช้ค่าเริ่มต้นของเซิร์ฟเวอร์ การบันทึกหน้านี้จะแทนที่ค่าเดิม",
		"%d replies per %s": "%d ครั้งต่อ %s",
	})
}
