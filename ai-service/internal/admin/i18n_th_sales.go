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
		"The bot saw a payment slip in these chats. Check the money, then confirm it in the chat with a private note: #paid and the amount, for example #paid 3,900. The amount below is what the bot read from the image, not checked.": "บอทเห็นสลิปโอนเงินในแชตเหล่านี้ ตรวจยอดเงินเข้าก่อน แล้วยืนยันในแชตด้วยโน้ตส่วนตัว: #paid ตามด้วยยอด เช่น #paid 3,900 ยอดด้านล่างคือที่บอทอ่านจากรูป ยังไม่ได้ตรวจ",
		"No slips waiting.":                 "ไม่มีสลิปรอตรวจ",
		"Paid chats":                        "แชตที่ชำระแล้ว",
		"no amount":                         "ไม่มียอด",
		"Staff":                             "ทีมงาน",
		"Not known":                         "ไม่ทราบ",
		"Showing the newest %d.":            "แสดง %d รายการล่าสุด",
		"No paid chats in this period yet.": "ยังไม่มีแชตที่ชำระเงินในช่วงนี้",
		"How sales are counted":             "นับยอดขายอย่างไร",
		"A chat counts as paid when staff write a private note #paid with the amount (for example #paid 3,900; #unpaid takes it back). The bot confirms with a note.":                                                                              "แชตนับว่าชำระแล้วเมื่อทีมงานเขียนโน้ตส่วนตัว #paid ตามด้วยยอด (เช่น #paid 3,900; #unpaid คือยกเลิก) บอทจะตอบยืนยันด้วยโน้ต",
		"On Facebook, Instagram and LINE chats, the label paid with the conversation attribute Paid amount (paid_amount, in baht) works too. Website-chat visitors can set labels and attributes themselves, so there only the #paid note counts.": "แชต Facebook, Instagram และ LINE ใช้ป้าย paid กับช่อง Paid amount ของแชต (paid_amount หน่วยบาท) ได้ด้วย ส่วนแชตหน้าเว็บ ผู้เข้าชมตั้งป้ายและช่องเองได้ จึงนับเฉพาะโน้ต #paid",
		"Closed by the bot: before the payment, the bot handed nothing to staff and no staff member replied to the customer. Otherwise it counts as closed with staff.":                                                                            "บอทปิดการขายเอง: ก่อนลูกค้าชำระเงิน บอทไม่ได้ส่งต่อให้ทีมงานและไม่มีทีมงานตอบลูกค้า ถ้าไม่ใช่ นับว่าทีมงานปิดการขาย",
		"Removing the label paid, or a #unpaid note, takes the chat out of the totals.":                                                                                                                                                            "ถ้าเอาป้าย paid ออก หรือเขียนโน้ต #unpaid แชตนั้นจะไม่นับในยอดรวม",
	})
}
