package admin

// Thai lines for the Connect AI tools page (see i18n_th.go). Issue: DEV-Johnstocker/chatwoot#1
func init() {
	addThai(map[string]string{
		"Connect AI tools · Pronto": "เชื่อมต่อเครื่องมือ AI · Pronto",
		"Let Claude, Codex or another AI tool read and test this knowledge base, suggest changes for your approval, and delete documents or sync website pages (MCP). Each access token works for one account only.": "ให้ Claude, Codex หรือเครื่องมือ AI อื่นอ่านและทดสอบฐานความรู้นี้ เสนอการแก้ไขให้คุณอนุมัติ และลบเอกสารหรือซิงก์หน้าเว็บไซต์ได้ (MCP) โทเคนแต่ละตัวใช้ได้กับบัญชีเดียวเท่านั้น",
		"Connections are off.":                  "ปิดการเชื่อมต่ออยู่",
		"Set %s for ai-service and restart it.": "ตั้งค่า %s ใน ai-service แล้วรีสตาร์ต",

		// The new token
		"Copy this token now": "คัดลอกโทเคนนี้ตอนนี้เลย",
		"It is shown only once. Anyone holding it can read, delete, sync and reschedule account %d's knowledge and suggest changes to it and the business profile, so keep it out of chats and screenshots. Lost it? Revoke it and create another.": "โทเคนแสดงเพียงครั้งเดียว ใครที่มีโทเคนนี้จะอ่าน ลบ ซิงก์ และเปลี่ยนรอบซิงก์ความรู้ของบัญชี %d ได้ และเสนอการแก้ไขความรู้กับโปรไฟล์ธุรกิจได้ อย่าวางในแชทหรือติดในภาพหน้าจอ ถ้าทำหาย ให้เพิกถอนแล้วสร้างใหม่",
		"It is shown only once. Anyone holding it can read account %d's knowledge, so keep it out of chats and screenshots. Lost it? Revoke it and create another.":                                                                                 "โทเคนแสดงเพียงครั้งเดียว ใครที่มีโทเคนนี้จะอ่านความรู้ของบัญชี %d ได้ อย่าวางในแชทหรือติดในภาพหน้าจอ ถ้าทำหาย ให้เพิกถอนแล้วสร้างใหม่",
		"Token for “%s”":                     "โทเคนสำหรับ “%s”",
		"Copy":                               "คัดลอก",
		"1. Save the token on your computer": "1. บันทึกโทเคนไว้ในคอมพิวเตอร์ของคุณ",
		"(Mac) in a text editor, add this line, save, and open a new terminal. Typing it into a terminal would leave it in the shell history.": "(Mac) ด้วยโปรแกรมแก้ไขข้อความ เพิ่มบรรทัดนี้ บันทึก แล้วเปิดเทอร์มินัลใหม่ ถ้าพิมพ์ลงเทอร์มินัลตรง ๆ โทเคนจะค้างอยู่ในประวัติคำสั่ง",
		"Add to": "เพิ่มลงใน",
		"Then ask your AI, for example: “List my knowledge base and tell me what is expired or never used.”": "จากนั้นลองสั่ง AI เช่น “แสดงรายการฐานความรู้ของฉัน แล้วบอกว่าอันไหนหมดอายุหรือไม่เคยถูกใช้”",

		// Apps that sign in
		"Claude or ChatGPT app (sign in, no token)": "แอป Claude หรือ ChatGPT (ลงชื่อเข้าใช้ ไม่ต้องใช้โทเคน)",
		"In the app, add a custom connector with this address. It opens a page where the person signs in with Cloudflare Access (their own email) and chooses one of their accounts below.": "ในแอป ให้เพิ่ม custom connector ด้วยที่อยู่นี้ ระบบจะเปิดหน้าให้คนนั้นลงชื่อเข้าใช้ผ่าน Cloudflare Access (ด้วยอีเมลของตัวเอง) แล้วเลือกบัญชีของเขาด้านล่าง",
		"In the app, add a custom connector with this address. It opens a page here where you enter the admin password and choose the account and access.":                                  "ในแอป ให้เพิ่ม custom connector ด้วยที่อยู่นี้ ระบบจะเปิดหน้านี้ให้คุณใส่รหัสผ่านแอดมิน แล้วเลือกบัญชีและสิทธิ์",
		"Connected app": "แอปที่เชื่อมต่อ",
		"Connected by":  "เชื่อมต่อโดย",
		"Access":        "สิทธิ์",
		"Connected":     "เชื่อมต่อเมื่อ",
		"Last used":     "ใช้ล่าสุด",
		"Actions":       "การดำเนินการ",
		"Disconnected":  "ยกเลิกการเชื่อมต่อแล้ว",
		"Disconnect %s? It loses access at once.": "ยกเลิกการเชื่อมต่อ %s ใช่ไหม แอปจะเข้าถึงไม่ได้ทันที",
		"Disconnect %s":          "ยกเลิกการเชื่อมต่อ %s",
		"Disconnect":             "ยกเลิกการเชื่อมต่อ",
		"Owner (admin password)": "เจ้าของ (รหัสผ่านแอดมิน)",

		// Team
		"Team for account %d": "ทีมของบัญชี %d",
		"People who may connect Claude or ChatGPT to this account with their own sign-in. Add their email here and in your Cloudflare Access policy. Removing someone disconnects the apps they connected.": "คนที่เชื่อมต่อ Claude หรือ ChatGPT กับบัญชีนี้ได้ด้วยการลงชื่อเข้าใช้ของตัวเอง เพิ่มอีเมลของเขาที่นี่และใน policy ของ Cloudflare Access ด้วย ถ้านำใครออก แอปที่เขาเชื่อมต่อไว้จะถูกยกเลิกการเชื่อมต่อ",
		"Email": "อีเมล",
		"Added": "เพิ่มเมื่อ",
		"Remove %s from the team? Apps they connected stop working.": "นำ %s ออกจากทีมใช่ไหม แอปที่เขาเชื่อมต่อไว้จะใช้ไม่ได้",
		"Remove %s":                       "นำ %s ออก",
		"Remove":                          "นำออก",
		"No one yet. Add yourself first.": "ยังไม่มีใคร เพิ่มตัวคุณเองก่อน",
		"Read only":                       "อ่านอย่างเดียว",
		"Read and write":                  "อ่านและแก้ไข",
		"Add to team":                     "เพิ่มเข้าทีม",

		// Tokens
		"Access tokens for account %d":                              "โทเคนเข้าถึงของบัญชี %d",
		"For Claude Code, Codex and other tools that take a token.": "สำหรับ Claude Code, Codex และเครื่องมืออื่นที่ใช้โทเคน",
		"Name":          "ชื่อ",
		"Created":       "สร้างเมื่อ",
		"Expires":       "หมดอายุ",
		"Revoked":       "เพิกถอนแล้ว",
		"Expired":       "หมดอายุแล้ว",
		"Never|expires": "ไม่หมดอายุ",
		"Never":         "ไม่เคย",
		"Revoke the token “%s”? Tools using it stop working at once.": "เพิกถอนโทเคน “%s” ใช่ไหม เครื่องมือที่ใช้โทเคนนี้จะใช้ไม่ได้ทันที",
		"Revoke %s": "เพิกถอน %s",
		"Revoke":    "เพิกถอน",
		"No AI tool is connected yet. Create a token to connect one.": "ยังไม่มีเครื่องมือ AI ที่เชื่อมต่อ สร้างโทเคนเพื่อเชื่อมต่อ",
		"What a connected AI can do":                                  "AI ที่เชื่อมต่อทำอะไรได้บ้าง",
		"Read only:":                                                  "อ่านอย่างเดียว:",
		"list documents with status and usage, read one, find near-copies, read the business profile, list website pages and data files, see recent changes, and ask the bot a test question.": "ดูรายการเอกสารพร้อมสถานะและการใช้งาน อ่านเอกสาร หาเอกสารที่ซ้ำกัน อ่านโปรไฟล์ธุรกิจ ดูรายการหน้าเว็บไซต์และไฟล์ข้อมูล ดูการเปลี่ยนแปลงล่าสุด และถามคำถามทดสอบกับบอต",
		"also: suggest a new or changed document (by its key, for example":                           "ทำได้เพิ่ม: เสนอเอกสารใหม่หรือแก้เอกสาร (ตามคีย์ เช่น",
		") or a change to the business name, about, tone and rules. These wait for your approval on": ") หรือเสนอแก้ชื่อธุรกิจ คำอธิบาย น้ำเสียง และกฎ ทั้งหมดนี้จะรอคุณอนุมัติในหน้า",
		"; customers keep seeing the current text until you approve.":                                " ลูกค้าจะยังเห็นข้อความเดิมจนกว่าคุณจะอนุมัติ",
		"Deleting a document waits for your approval on the AI approvals page, like other edits. Syncing or rescheduling a website page or data file takes effect at once (a data file syncs in the background). Approved deletes and schedule changes show under Recent changes on Home; what a sync changes shows as “website sync”. The history is kept for 180 days by default.": "การลบเอกสารจะรอคุณอนุมัติที่หน้าอนุมัติการแก้ไขจาก AI เหมือนการแก้ไขอื่น ส่วนการซิงก์หรือเปลี่ยนรอบซิงก์หน้าเว็บไซต์หรือไฟล์ข้อมูลมีผลทันที (ไฟล์ข้อมูลจะซิงก์อยู่เบื้องหลัง) การลบที่อนุมัติแล้วและการเปลี่ยนรอบจะแสดงในการเปลี่ยนแปลงล่าสุดที่หน้าแรก ส่วนสิ่งที่ซิงก์เปลี่ยนจะแสดงเป็น “website sync” โดยปกติจะเก็บประวัติไว้ 180 วัน",

		// New token form
		"New token":                       "โทเคนใหม่",
		"Claude Code on my Mac":           "Claude Code บน Mac ของฉัน",
		"Safe for reviewing and testing.": "ปลอดภัยสำหรับการตรวจและทดสอบ",
		"Edits and deletes wait for your approval. Syncs apply at once.": "การแก้ไขและการลบจะรอคุณอนุมัติ การซิงก์มีผลทันที",
		"Expires after": "หมดอายุหลัง",
		"%d days":       "%d วัน",
		"1 year":        "1 ปี",
		"Create token":  "สร้างโทเคน",

		// Notices and errors
		"Revoked. AI tools using that token are cut off immediately.":                                                                                          "เพิกถอนแล้ว เครื่องมือ AI ที่ใช้โทเคนนั้นถูกตัดการเข้าถึงทันที",
		"Team updated. Removing someone also disconnects the apps they connected to this account.":                                                             "อัปเดตทีมแล้ว การนำใครออกจะยกเลิกการเชื่อมต่อแอปที่เขาเชื่อมกับบัญชีนี้ด้วย",
		"Could not load the connections. Check the database connection.":                                                                                       "โหลดการเชื่อมต่อไม่ได้ ตรวจการเชื่อมต่อฐานข้อมูล",
		"That form was already sent (for example by reloading the page), so no token was made this time. If you did not copy a token, create one again below.": "ฟอร์มนี้ถูกส่งไปแล้ว (เช่น จากการรีโหลดหน้า) รอบนี้จึงไม่ได้สร้างโทเคน ถ้ายังไม่ได้คัดลอกโทเคน ให้สร้างใหม่ด้านล่าง",
		"Enter a valid email address.":                                    "ใส่อีเมลให้ถูกต้อง",
		"Could not change the team. Try again.":                           "แก้ไขทีมไม่ได้ ลองอีกครั้ง",
		"That app is already disconnected or belongs to another account.": "แอปนี้ถูกยกเลิกการเชื่อมต่อแล้ว หรือเป็นของบัญชีอื่น",
		"That token is already revoked or belongs to another account.":    "โทเคนนี้ถูกเพิกถอนแล้ว หรือเป็นของบัญชีอื่น",
	})
}
