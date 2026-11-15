package admin

// Thai lines for the unanswered-questions page (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"Unanswered questions · Pronto": "คำถามที่บอตยังตอบไม่ได้ · Pronto",
		"Unanswered questions":          "คำถามที่บอตยังตอบไม่ได้",
		"Chats the bot passed to your team only because it did not know the answer (label kb-gap in Chatwoot). Add the answer once and the bot can reply next time.": "แชทที่บอตส่งต่อให้ทีมงานเพราะไม่มีข้อมูลตอบ (ป้าย kb-gap ใน Chatwoot) เพิ่มคำตอบครั้งเดียว ครั้งหน้าบอตตอบเองได้",
		"The question list is off.": "รายการคำถามยังใช้งานไม่ได้",
		"No unanswered questions. The bot had an answer for everything it was asked.": "ไม่มีคำถามค้าง บอตมีคำตอบให้ทุกคำถาม",
		"Chat #%d":      "แชท #%d",
		"Open chat #%d": "เปิดแชท #%d",
		"Bot's summary": "บอตสรุปว่า",
		"The bot's note was not found in the latest messages; open the chat to see the question.": "ไม่พบโน้ตของบอตในข้อความล่าสุด เปิดแชทเพื่อดูคำถาม",
		"Your team replied":      "ทีมงานตอบไปว่า",
		"Done, no answer needed": "เสร็จแล้ว ไม่ต้องเพิ่มคำตอบ",
		"Turn into an answer":    "เพิ่มเป็นคำตอบ",
		"Showing the newest %d. Handle these to see older ones.":                                            "แสดง %d รายการล่าสุด จัดการรายการเหล่านี้แล้วจะเห็นรายการเก่ากว่า",
		"Answer added to the knowledge base, and the chat is off the list.":                                 "เพิ่มคำตอบในฐานความรู้แล้ว และนำแชทออกจากรายการแล้ว",
		"Answer added to the knowledge base. The chat could not be taken off the list; mark it done below.": "เพิ่มคำตอบในฐานความรู้แล้ว แต่นำแชทออกจากรายการไม่ได้ กด “เสร็จแล้ว” ด้านล่าง",
		"Marked done. The chat now has the label kb-gap-done in Chatwoot.":                                  "เรียบร้อย แชทนี้มีป้าย kb-gap-done ใน Chatwoot แล้ว",
		"Could not read the chats from Chatwoot. Check that the AI Reader user still has access.":           "อ่านแชทจาก Chatwoot ไม่ได้ ตรวจสอบว่าผู้ใช้ AI Reader ยังเข้าถึงกล่องข้อความได้",
		"Could not reach Chatwoot. Nothing was changed; try again.":                                         "ติดต่อ Chatwoot ไม่ได้ ยังไม่มีอะไรเปลี่ยน ลองอีกครั้ง",
		"Chat #%d is no longer on the gap list. Someone may have handled it already.":                       "แชท #%d ไม่อยู่ในรายการแล้ว อาจมีคนจัดการไปแล้ว",
		"Write the answer to this customer question below. Adding it takes chat #%d off the gap list.":      "เขียนคำตอบของคำถามลูกค้าด้านล่าง เมื่อเพิ่มแล้ว แชท #%d จะออกจากรายการคำถามที่บอตยังตอบไม่ได้",
	})
}
