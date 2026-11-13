package admin

// Thai lines for the bot on/off page (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"Bot on/off · Pronto": "เปิด/ปิดบอต · Pronto",
		"Bot on/off":          "เปิด/ปิดบอต",
		"One switch for this account's chat bot. While the bot is off, each new customer chat goes straight to your team in Chatwoot (Open) with a note, and the bot sends nothing.": "สวิตช์เดียวสำหรับบอตแชทของบัญชีนี้ ระหว่างที่บอตปิด แชทใหม่ของลูกค้าจะส่งตรงถึงทีมงานใน Chatwoot (เปิดอยู่) พร้อมโน้ต และบอตจะไม่ส่งข้อความใดๆ",
		"The bot switch is off.": "สวิตช์บอตใช้งานไม่ได้",
		"Could not load the bot switch. Check the database connection.": "โหลดสวิตช์บอตไม่ได้ ตรวจสอบการเชื่อมต่อฐานข้อมูล",
		"Could not save the bot switch. Check the database connection.": "บันทึกสวิตช์บอตไม่ได้ ตรวจสอบการเชื่อมต่อฐานข้อมูล",
		"Saved. It applies from the next customer message.":             "บันทึกแล้ว มีผลตั้งแต่ข้อความถัดไปของลูกค้า",
		"Right now the bot is answering.":                               "ตอนนี้บอตกำลังตอบลูกค้า",
		"Right now the bot is off. Your team gets every new chat.":      "ตอนนี้บอตปิดอยู่ ทีมงานจะได้รับแชทใหม่ทุกแชท",
		"Time now: %s (%s).":   "เวลาตอนนี้: %s (%s)",
		"Last changed %s.":     "แก้ไขล่าสุด %s",
		"Answers at any time.": "ตอบลูกค้าตลอดเวลา",
		"On during set hours":  "เปิดตามเวลาที่ตั้ง",
		"Answers only in the hours below; your team gets chats at other times.": "ตอบเฉพาะช่วงเวลาด้านล่าง นอกเวลานั้นทีมงานรับแชทเอง",
		"Answers nothing; your team gets every new chat.":                       "ไม่ตอบเลย ทีมงานรับแชทใหม่ทุกแชท",
		"Bot hours": "เวลาทำงานของบอต",
		"Used only when the bot is on during set hours. An end time earlier than the start runs past midnight (for example 18:00 to 09:00 covers the night). The same start and end time means the whole day.": "ใช้เฉพาะเมื่อเลือก “เปิดตามเวลาที่ตั้ง” ถ้าเวลาสิ้นสุดน้อยกว่าเวลาเริ่ม จะต่อเลยเที่ยงคืนไปวันถัดไป (เช่น 18:00 ถึง 09:00 คือช่วงกลางคืน) ถ้าเวลาเริ่มและสิ้นสุดเท่ากัน หมายถึงทั้งวัน",
		"Time zone": "เขตเวลา",
		"Day":       "วัน",
		"Bot on":    "บอตเปิด",
		"To":        "ถึง",
		"%s from":   "%s เริ่ม",
		"%s to":     "%s ถึง",
		"next day":  "วันถัดไป",
		"Chats already handed to your team stay with them. Chatwoot's own business hours only send an away message; they do not switch the bot.": "แชทที่ส่งให้ทีมงานแล้วจะอยู่กับทีมงานต่อ ส่วนเวลาทำการในตั้งค่าของ Chatwoot มีไว้ส่งข้อความแจ้งว่าไม่อยู่เท่านั้น ไม่ได้เปิดหรือปิดบอต",
		"Monday":    "วันจันทร์",
		"Tuesday":   "วันอังคาร",
		"Wednesday": "วันพุธ",
		"Thursday":  "วันพฤหัสบดี",
		"Friday":    "วันศุกร์",
		"Saturday":  "วันเสาร์",
		"Sunday":    "วันอาทิตย์",
	})
}
