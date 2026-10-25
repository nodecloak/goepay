package backoffice

func collectionStyleFields() []Field {
	return []Field{
		text("name", "风格标识", true, 100), text("label", "显示名称", false, 100), integer("sort", "排序", 0, 1000000), flag("enabled", "启用", true), text("url", "图片地址", false, 2048), integer("img_width", "画布宽度", 64, 4096), integer("img_height", "画布高度", 64, 4096), integer("qr_width", "二维码宽度", 32, 4096), integer("qr_height", "二维码高度", 32, 4096), integer("qr_left", "二维码左边距", 0, 4096), integer("qr_top", "二维码上边距", 0, 4096), text("foreground", "前景色", false, 7), text("background", "二维码背景色", false, 11), text("font", "字体", false, 100), text("font_color", "文字颜色", false, 11), text("rec_name_left", "文字横坐标（空为居中）", false, 4), integer("rec_name_top", "文字纵坐标", 0, 4096), integer("word", "收款文字模式", 0, 2),
		// Historical simple-color styles remain readable and editable through their
		// old schema. They acquire layout geometry only through an explicit edit.
		text("title", "旧展示标题", false, 100), text("background_color", "旧页面背景颜色", false, 7), text("text_color", "旧页面文字颜色", false, 7), {Name: "logo_url", Label: "旧Logo URL", Type: "url", MaxLength: 2048},
	}
}
