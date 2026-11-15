package admin

// Thai lines for the sales dashboard (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"Sales · Pronto": "ยอดขาย · Pronto",
		"Sales":          "ยอดขาย",
		"Chats that paid, how much they brought in, and how many the bot closed without help from staff.": "แชตที่ชำระเงินแล้ว ยอดรวม และจำนวนที่บอทปิดการขายได้เองโดยไม่ต้องให้ทีมงานช่วย",
		"Sales are off.": "ยอดขายปิดอยู่",
		"Period":         "ช่วงเวลา",
		"This month":     "เดือนนี้",
		"Last month":     "เดือนที่แล้ว",
		"Last 30 days":   "30 วันล่าสุด",
		"Could not load the sales. Check the database connection.": "โหลดยอดขายไม่ได้ ตรวจการเชื่อมต่อฐานข้อมูล",
		"Revenue":                "ยอดขาย",
		"%d paid chats":          "ชำระแล้ว %d แชต",
		"%d without an amount":   "ไม่มียอดเงิน %d แชต",
		"Closed by the bot":      "บอทปิดการขายเอง",
		"%d%% of sales":          "%d%% ของยอดขาย",
		"Closed with staff":      "ปิดการขายโดยทีมงาน",
		"%d not known":           "ไม่ทราบ %d แชต",
		"Slips to check":         "สลิปรอตรวจ",
		"label paid-unconfirmed": "ป้าย paid-unconfirmed",
		"The bot saw a payment slip in these chats. Check the money, then in Chatwoot add the label paid and set the conversation attribute Paid amount. The amount below is what the bot read from the image, not checked.": "บอทเห็นสลิปโอนเงินในแชตเหล่านี้ ตรวจยอดเงินเข้าก่อน แล้วใน Chatwoot ให้ใส่ป้าย paid และกรอกช่อง Paid amount ของแชต ยอดด้านล่างคือที่บอทอ่านจากรูป ยังไม่ได้ตรวจ",
		"No slips waiting.":                 "ไม่มีสลิปรอตรวจ",
		"Paid chats":                        "แชตที่ชำระแล้ว",
		"no amount":                         "ไม่มียอด",
		"Staff":                             "ทีมงาน",
		"Not known":                         "ไม่ทราบ",
		"Showing the newest %d.":            "แสดง %d รายการล่าสุด",
		"No paid chats in this period yet.": "ยังไม่มีแชตที่ชำระเงินในช่วงนี้",
		"How sales are counted":             "นับยอดขายอย่างไร",
		"A chat counts as paid when staff add the label paid in Chatwoot. The amount is the conversation attribute Paid amount (paid_amount, in baht).":                 "แชตนับว่าชำระแล้วเมื่อทีมงานใส่ป้าย paid ใน Chatwoot ยอดเงินมาจากช่อง Paid amount ของแชต (paid_amount หน่วยบาท)",
		"Closed by the bot: before the payment, the bot handed nothing to staff and no staff member replied to the customer. Otherwise it counts as closed with staff.": "บอทปิดการขายเอง: ก่อนลูกค้าชำระเงิน บอทไม่ได้ส่งต่อให้ทีมงานและไม่มีทีมงานตอบลูกค้า ถ้าไม่ใช่ นับว่าทีมงานปิดการขาย",
		"Removing the label paid takes the chat out of the totals.": "ถ้าเอาป้าย paid ออก แชตนั้นจะไม่นับในยอดรวม",
	})
}
