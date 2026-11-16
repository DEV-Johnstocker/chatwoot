package admin

// Thai lines for the bot alert banner on Home (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"Bot alert: %s": "แจ้งเตือนบอท: %s",
		"Since %s. Affects every account on this server.": "ตั้งแต่ %s มีผลกับทุกบัญชีบนเซิร์ฟเวอร์นี้",
		"Bot replies are not reaching Chatwoot":           "คำตอบของบอทส่งเข้า Chatwoot ไม่สำเร็จ",
		"The AI model is failing":                         "โมเดล AI ขัดข้อง",
		"The bot is overloaded (queue full)":              "บอทรับงานไม่ทัน (คิวเต็ม)",
		"Unusually many chats handed to staff":            "แชทถูกส่งต่อให้ทีมงานมากผิดปกติ",
		"Chats hit the reply limit":                       "มีแชทที่บอทตอบครบจำนวนที่กำหนดแล้ว",
	})
}
