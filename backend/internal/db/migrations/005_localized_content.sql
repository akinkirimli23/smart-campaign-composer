-- Content per language (DECISIONS 30).
--
-- campaigns.content used to be one set of texts: {"sms": ..., "email": {...}}.
-- It now holds one set per language and names the default:
-- {"default": "tr", "variants": {"tr": {...}, "en": {...}}}.
-- Existing campaigns were written in one language, Turkish by default; wrap
-- them so the worker reads every campaign the same way, including scheduled
-- ones that have not been sent yet.
UPDATE campaigns
SET content = jsonb_build_object('default', 'tr', 'variants', jsonb_build_object('tr', content))
WHERE NOT (content ? 'variants');
