package v2go

import "freenode/pkg/models"

// DefaultSources returns the default sources for conective
func DefaultSources() []models.Source {
	return []models.Source{
		// Active default sources
		{Name: "freedom", URL: "https://raw.githubusercontent.com/MahanKenway/Freedom-V2Ray/main/configs/mix.txt", Format: "auto", Enabled: true},
		{Name: "v2go", URL: "https://raw.githubusercontent.com/Danialsamadi/v2go/main/AllConfigsSub.txt", Format: "auto", Enabled: true},

		// Base64 subscription links (disabled by default)
		{Name: "ALIILAPRO", URL: "https://raw.githubusercontent.com/ALIILAPRO/v2rayNG-Config/main/sub.txt", Format: "base64", Enabled: false},
		{Name: "mfuu", URL: "https://raw.githubusercontent.com/mfuu/v2ray/master/v2ray", Format: "base64", Enabled: false},
		{Name: "ts-sf", URL: "https://raw.githubusercontent.com/ts-sf/fly/main/v2", Format: "base64", Enabled: false},
		{Name: "MahsaNet MCI 1", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mci/sub_1.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet MCI 2", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mci/sub_2.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet MCI 3", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mci/sub_3.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet App", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/app/sub.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet MTN 1", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mtn/sub_1.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet MTN 2", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mtn/sub_2.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet MTN 3", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mtn/sub_3.txt", Format: "base64", Enabled: false},
		{Name: "MahsaNet MTN 4", URL: "https://raw.githubusercontent.com/mahsanet/MahsaFreeConfig/refs/heads/main/mtn/sub_4.txt", Format: "base64", Enabled: false},
		{Name: "VPN-Fail", URL: "https://raw.githubusercontent.com/yebekhe/vpn-fail/refs/heads/main/sub-link", Format: "base64", Enabled: false},

		// Direct text / mixed links (disabled by default)
		{Name: "Alicivil Worker", URL: "https://v2.alicivil.workers.dev", Format: "text", Enabled: false},
		{Name: "TGParse Mixed", URL: "https://raw.githubusercontent.com/Surfboardv2ray/TGParse/main/splitted/mixed", Format: "text", Enabled: false},
		{Name: "PSG Xray Mix", URL: "https://raw.githubusercontent.com/itsyebekhe/PSG/main/lite/subscriptions/xray/normal/mix", Format: "text", Enabled: false},
		{Name: "GO_V2rayCollector", URL: "https://raw.githubusercontent.com/HosseinKoofi/GO_V2rayCollector/main/mixed_iran.txt", Format: "text", Enabled: false},
		{Name: "v2rayExtractor Mix", URL: "https://raw.githubusercontent.com/arshiacomplus/v2rayExtractor/refs/heads/main/mix/sub.html", Format: "text", Enabled: false},
		{Name: "Rayan-Config Proxy", URL: "https://raw.githubusercontent.com/Rayan-Config/C-Sub/refs/heads/main/configs/proxy.txt", Format: "text", Enabled: false},
		{Name: "Shadowsocks Eternity", URL: "https://raw.githubusercontent.com/mahdibland/ShadowsocksAggregator/master/Eternity.txt", Format: "text", Enabled: false},
		{Name: "Everyday-VPN", URL: "https://raw.githubusercontent.com/Everyday-VPN/Everyday-VPN/main/subscription/main.txt", Format: "text", Enabled: false},
		{Name: "MahsaNet Topic Xray", URL: "https://raw.githubusercontent.com/MahsaNetConfigTopic/config/refs/heads/main/xray_final.txt", Format: "text", Enabled: false},
		{Name: "Epodonios All Sub", URL: "https://github.com/Epodonios/v2ray-configs/raw/main/All_Configs_Sub.txt", Format: "text", Enabled: false},
		{Name: "V2RayRoot VLESS", URL: "https://raw.githubusercontent.com/V2RayRoot/V2RayConfig/refs/heads/main/Config/vless.txt", Format: "text", Enabled: false},
		{Name: "V2RayRoot VMess", URL: "https://raw.githubusercontent.com/V2RayRoot/V2RayConfig/refs/heads/main/Config/vmess.txt", Format: "text", Enabled: false},
		{Name: "ebrasha Free Public", URL: "https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/all_extracted_configs.txt", Format: "text", Enabled: false},
		{Name: "ScrapeByCountry VLESS", URL: "https://raw.githubusercontent.com/miladtahanian/V2RayScrapeByCountry/refs/heads/main/output_configs/Vless.txt", Format: "text", Enabled: false},
		{Name: "ScrapeByCountry VMess", URL: "https://raw.githubusercontent.com/miladtahanian/V2RayScrapeByCountry/refs/heads/main/output_configs/Vmess.txt", Format: "text", Enabled: false},
		{Name: "miladtahanian All Configs", URL: "https://raw.githubusercontent.com/miladtahanian/V2ray-Config/main/All_Configs_Sub.txt", Format: "text", Enabled: false},
		{Name: "SoliSpirit Configs", URL: "https://raw.githubusercontent.com/SoliSpirit/v2ray-configs/refs/heads/main/all_configs.txt", Format: "text", Enabled: false},
		{Name: "Kolandone Collector", URL: "https://raw.githubusercontent.com/Kolandone/v2raycollector/refs/heads/main/config.txt", Format: "text", Enabled: false},
		{Name: "mohamadfg VLESS", URL: "https://raw.githubusercontent.com/mohamadfg-dev/telegram-v2ray-configs-collector/refs/heads/main/category/vless.txt", Format: "text", Enabled: false},
		{Name: "mohamadfg VMess", URL: "https://raw.githubusercontent.com/mohamadfg-dev/telegram-v2ray-configs-collector/refs/heads/main/category/vmess.txt", Format: "text", Enabled: false},
		{Name: "mohamadfg Trojan", URL: "https://raw.githubusercontent.com/mohamadfg-dev/telegram-v2ray-configs-collector/refs/heads/main/category/trojan.txt", Format: "text", Enabled: false},
		{Name: "Surfboard TGParse", URL: "https://raw.githubusercontent.com/Surfboardv2ray/TGParse/refs/heads/main/configtg.txt", Format: "text", Enabled: false},
		{Name: "Kamaji Merged", URL: "https://raw.githubusercontent.com/shabane/kamaji/refs/heads/master/hub/merged.txt", Format: "text", Enabled: false},
		{Name: "Black VLESS RUS", URL: "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/BLACK_VLESS_RUS.txt", Format: "text", Enabled: false},
		{Name: "Black SS+All RUS", URL: "https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/refs/heads/main/BLACK_SS+All_RUS.txt", Format: "text", Enabled: false},
	}
}
