package admin

// Thai lines for the AI approvals page (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"AI approvals · Pronto": "อนุมัติการแก้ไขจาก AI · Pronto",
		"Changes that connected AI apps want to make to the business profile and the knowledge base. Customers keep seeing the current text until you approve.": "การแก้ไขโปรไฟล์ธุรกิจและฐานความรู้ที่แอป AI ที่เชื่อมต่อไว้เสนอมา ลูกค้าจะยังเห็นข้อความเดิมจนกว่าคุณจะอนุมัติ",
		"Approvals are off.": "ปิดการอนุมัติอยู่",
		"They need":          "ต้องตั้งค่า",
		"and":                "และ",
		"for ai-service.":    "ใน ai-service ก่อน",
		"Approved. The bot uses it from the next message.": "อนุมัติแล้ว บอตจะใช้ตั้งแต่ข้อความถัดไป",
		"Rejected. Nothing changed.":                       "ปฏิเสธแล้ว ไม่มีอะไรเปลี่ยน",
		"Nothing is waiting for approval.":                 "ไม่มีรายการรออนุมัติ",
		"New document":                                     "เอกสารใหม่",
		"(AI app)":                                         "(แอป AI)",
		"Unchanged":                                        "ไม่เปลี่ยน",
		"Now":                                              "ตอนนี้",
		"(empty)":                                          "(ว่าง)",
		"Proposed":                                         "ที่เสนอ",
		"Reject":                                           "ปฏิเสธ",
		"Approve":                                          "อนุมัติ",

		// What a change touches
		"Knowledge: %s":             "ความรู้: %s",
		"Business name":             "ชื่อธุรกิจ",
		"What the business does":    "ธุรกิจทำอะไร",
		"Tone and style":            "น้ำเสียงและสไตล์",
		"Rules":                     "กฎ",
		"Title":                     "หัวข้อ",
		"Text":                      "ข้อความ",
		"Used in":                   "ใช้ใน",
		"Only inbox %d":             "เฉพาะกล่องข้อความ %d",
		"All inboxes":               "ทุกกล่องข้อความ",
		"Kind":                      "ประเภท",
		"FAQ (question and answer)": "FAQ (คำถามและคำตอบ)",
		"Dates":                     "วันที่",
		"from %s to %s":             "ตั้งแต่ %s ถึง %s",
		"now":                       "ตอนนี้",
		"no end":                    "ไม่มีวันสิ้นสุด",

		// Errors
		"The app changed this after you opened it. Nothing was applied; check the new version below.":     "แอปแก้รายการนี้หลังจากคุณเปิดหน้า ยังไม่มีอะไรถูกนำไปใช้ ตรวจเวอร์ชันใหม่ด้านล่าง",
		"That change is no longer waiting. It may have been approved or rejected already.":                "รายการนี้ไม่ได้รออยู่แล้ว อาจถูกอนุมัติหรือปฏิเสธไปแล้ว",
		"Not applied. The change still waits; try again, and check the service logs if it keeps failing.": "ยังไม่ได้นำไปใช้ รายการยังรออยู่ ลองอีกครั้ง ถ้ายังไม่ได้ให้ตรวจ log ของระบบ",
		"Could not load the waiting changes. Check the database connection.":                              "โหลดรายการที่รออนุมัติไม่ได้ ตรวจการเชื่อมต่อฐานข้อมูล",
	})
}
