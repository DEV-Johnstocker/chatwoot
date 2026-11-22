package admin

// Thai lines for the ads on the Leads and Sales pages (see i18n_th.go).
// Issues: DEV-Johnstocker/chatwoot#1, #156.
func init() {
	addThai(map[string]string{
		"Ad":                "โฆษณา",
		"Ad %s":             "โฆษณา %s",
		"Link: %s":          "ลิงก์: %s",
		"Link":              "ลิงก์",
		"Earlier: %s":       "ก่อนหน้า: %s",
		"Ads":               "โฆษณา",
		"Who gets the sale": "ยอดขายนับให้โฆษณาไหน",
		"Last ad":           "โฆษณาล่าสุด",
		"First ad":          "โฆษณาแรก",
		"A sale counts for the first ad or link the customer tapped before paying.": "ยอดขายนับให้โฆษณาหรือลิงก์แรกที่ลูกค้ากดก่อนชำระเงิน",
		"A sale counts for the last ad or link the customer tapped before paying.":  "ยอดขายนับให้โฆษณาหรือลิงก์ล่าสุดที่ลูกค้ากดก่อนชำระเงิน",
		"Ad or link": "โฆษณาหรือลิงก์",
		"Chats":      "แชต",
		"No ad":      "ไม่ได้มาจากโฆษณา",
		"No ad taps yet. They appear after someone messages the Page from an ad.":                                                                                             "ยังไม่มีคนกดโฆษณา จะขึ้นที่นี่เมื่อมีคนทักเพจจากโฆษณา",
		"Counts only chats that sent a message, so Ads Manager counts more. Chats: customers who tapped it in this period. Leads: of those, who asked about price or buying.": "นับเฉพาะแชตที่ลูกค้าส่งข้อความมา ตัวเลขใน Ads Manager จึงสูงกว่า แชต: ลูกค้าที่กดในช่วงเวลานี้ ลีด: ในจำนวนนั้น คนที่ถามราคาหรือการซื้อ",
		"Could not read the ads. Try again in a moment.":                                                                                                                      "อ่านข้อมูลโฆษณาไม่ได้ ลองใหม่อีกครั้งในอีกสักครู่",
	})
}
