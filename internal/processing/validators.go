package processing

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Checks that turn "looks like" into "is": checksums, check digits and
// structure tests. A rule names one by key (validator: "iban"): a failed check
// drops the match, a passed one raises its score. All of them are pure and
// fast.

var (
	nonDigits = regexp.MustCompile(`\D`)
	compactCh = regexp.MustCompile(`[\s.\-/]`)
	letters   = regexp.MustCompile(`[A-Z]`)
)

func digitsOf(value string) string { return nonDigits.ReplaceAllString(value, "") }

func compactOf(value string) string {
	return strings.ToUpper(compactCh.ReplaceAllString(value, ""))
}

func crossSum(number int) int {
	sum := 0
	for _, digit := range strconv.Itoa(number) {
		sum += int(digit - '0')
	}
	return sum
}

// letterNumber is A=1 … Z=26.
func letterNumber(char byte) int { return int(char) - 64 }

func luhn(digits string) bool {
	sum := 0
	for index := 0; index < len(digits); index++ {
		digit := int(digits[len(digits)-1-index] - '0')
		if index%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}

// Luhn is the exported check digit test.
func Luhn(digits string) bool { return luhn(digitsOf(digits)) }

var cardBrands = []struct {
	prefix  *regexp.Regexp
	lengths []int
}{
	{regexp.MustCompile(`^4`), []int{13, 16, 19}},
	{regexp.MustCompile(`^(5[1-5]|222[1-9]|22[3-9]\d|2[3-6]\d{2}|27[01]\d|2720)`), []int{16}},
	{regexp.MustCompile(`^3[47]`), []int{15}},
	{regexp.MustCompile(`^(6011|65|64[4-9]|622)`), []int{16, 17, 18, 19}},
	{regexp.MustCompile(`^3(0[0-5]|[689])`), []int{14, 15, 16, 17, 18, 19}},
	{regexp.MustCompile(`^35(2[89]|[3-8])`), []int{16, 17, 18, 19}},
	{regexp.MustCompile(`^(50|5[6-9]|6\d)`), []int{12, 13, 14, 15, 16, 17, 18, 19}},
}

// cardValid is Luhn plus a known issuer prefix with that issuer's length, so
// random long numbers (order ids, account parts) are not mistaken for cards.
func cardValid(value string) bool {
	digits := digitsOf(value)
	for _, brand := range cardBrands {
		if !brand.prefix.MatchString(digits) {
			continue
		}
		for _, length := range brand.lengths {
			if len(digits) == length && luhn(digits) {
				return true
			}
		}
		return false
	}
	return false
}

// CardValid is the exported card test.
func CardValid(value string) bool { return cardValid(value) }

func mod97(numeric string) int {
	remainder := 0
	for _, digit := range numeric {
		remainder = (remainder*10 + int(digit-'0')) % 97
	}
	return remainder
}

func ibanValid(value string) bool {
	compact := strings.ReplaceAll(value, " ", "")
	if len(compact) < 15 || len(compact) > 34 {
		return false
	}
	rearranged := compact[4:] + compact[:4]
	digits := letters.ReplaceAllStringFunc(rearranged, func(char string) string {
		return strconv.Itoa(int(char[0]) - 55)
	})
	return mod97(digits) == 1
}

// IBANValid is the exported IBAN test.
func IBANValid(value string) bool { return ibanValid(value) }

func entropy(value string) float64 {
	if value == "" {
		return 0
	}
	counts := map[rune]int{}
	for _, char := range value {
		counts[char]++
	}
	length := float64(len([]rune(value)))
	bits := 0.0
	for _, count := range counts {
		p := float64(count) / length
		bits -= p * math.Log2(p)
	}
	return bits
}

// Entropy is the exported Shannon entropy in bits per character.
func Entropy(value string) float64 { return entropy(value) }

var (
	dateLike    = regexp.MustCompile(`^\d{4}[-./]\d{1,2}[-./]\d{1,2}$`)
	phonePrefix = regexp.MustCompile(`^(\+|00|0|\()`)
	phonePunct  = regexp.MustCompile(`[\s/-]`)
)

func looksLikePhone(value string) bool {
	digits := digitsOf(value)
	if len(digits) < 8 || len(digits) > 15 {
		return false
	}
	trimmed := strings.TrimSpace(value)
	// Dates, versions and plain amounts are not phone numbers.
	if dateLike.MatchString(trimmed) {
		return false
	}
	return phonePrefix.MatchString(trimmed) || phonePunct.MatchString(value)
}

// iso7064Mod11_10 is ISO 7064 MOD 11,10: German tax ID and VAT ID check digits.
func iso7064Mod11_10(digits string) bool {
	product := 10
	for _, digit := range digits[:len(digits)-1] {
		sum := (int(digit-'0') + product) % 10
		if sum == 0 {
			sum = 10
		}
		product = (sum * 2) % 11
	}
	check := (11 - product) % 10
	return check == int(digits[len(digits)-1]-'0')
}

var steuerIDShape = regexp.MustCompile(`^[1-9]\d{10}$`)

// steuerID is the German tax ID: 11 digits, no leading 0, in the first ten one
// digit appears two or three times (three never all in a row) and every other
// digit at most once.
func steuerID(value string) bool {
	digits := digitsOf(value)
	if !steuerIDShape.MatchString(digits) {
		return false
	}
	counts := map[rune]int{}
	for _, digit := range digits[:10] {
		counts[digit]++
	}
	repeated := 0
	var repeatedDigit rune
	for digit, count := range counts {
		if count > 1 {
			repeated++
			repeatedDigit = digit
		}
	}
	if repeated != 1 || counts[repeatedDigit] > 3 {
		return false
	}
	if counts[repeatedDigit] == 3 {
		if strings.Contains(digits[:10], strings.Repeat(string(repeatedDigit), 3)) {
			return false
		}
	}
	return iso7064Mod11_10(digits)
}

func ustIDDe(value string) bool {
	digits := digitsOf(value)
	return len(digits) == 9 && iso7064Mod11_10(digits)
}

var (
	rvnrShape = regexp.MustCompile(`^(\d{8})([A-Z])(\d{2})(\d)$`)
	kvnrShape = regexp.MustCompile(`^([A-Z])(\d{8})(\d)$`)
)

// rvnr is the German pension number: area, birth date, initial letter, serial,
// check digit.
func rvnr(value string) bool {
	match := rvnrShape.FindStringSubmatch(compactOf(value))
	if match == nil {
		return false
	}
	digits := match[1] + leftPad2(letterNumber(match[2][0])) + match[3]
	weights := []int{2, 1, 2, 5, 7, 1, 2, 1, 2, 1, 2, 1}
	sum := 0
	for index, weight := range weights {
		sum += crossSum(weight * int(digits[index]-'0'))
	}
	return sum%10 == int(match[4][0]-'0')
}

func leftPad2(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

// kvnr is the German health insurance number: a letter and nine digits, the
// last of them the check digit.
func kvnr(value string) bool {
	match := kvnrShape.FindStringSubmatch(compactOf(value))
	if match == nil {
		return false
	}
	digits := leftPad2(letterNumber(match[1][0])) + match[2]
	sum := 0
	for index := 0; index < len(digits); index++ {
		factor := 1
		if index%2 == 1 {
			factor = 2
		}
		sum += crossSum(int(digits[index]-'0') * factor)
	}
	return sum%10 == int(match[3][0]-'0')
}

// icaoCheck is the ICAO 9303 7-3-1 check digit (passports, ID cards, MRZ).
func icaoCheck(chars string) int {
	weights := []int{7, 3, 1}
	sum := 0
	for index := 0; index < len(chars); index++ {
		char := chars[index]
		var number int
		switch {
		case char == '<':
			number = 0
		case char >= '0' && char <= '9':
			number = int(char - '0')
		default:
			number = int(char) - 55
		}
		sum += number * weights[index%3]
	}
	return sum % 10
}

var (
	idCardShape = regexp.MustCompile(`^[CFGHJKLMNPRTVWXYZ0-9]{9}\d$`)
	ahvShape    = regexp.MustCompile(`^756\d{10}$`)
	svnrShape   = regexp.MustCompile(`^\d{10}$`)
	bsnShape    = regexp.MustCompile(`^\d{9}$`)
	peselShape  = regexp.MustCompile(`^\d{11}$`)
	beShape     = regexp.MustCompile(`^\d{11}$`)
	nhsShape    = regexp.MustCompile(`^\d{10}$`)
	abaShape    = regexp.MustCompile(`^\d{9}$`)
	vinShape    = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`)
	nirShape    = regexp.MustCompile(`^[12]\d{14}$`)
	dniShape    = regexp.MustCompile(`^(\d{8})([A-Z])$`)
	fiscalShape = regexp.MustCompile(`^[A-Z]{6}[0-9LMNPQRSTUV]{2}[A-EHLMPRST][0-9LMNPQRSTUV]{2}[A-Z][0-9LMNPQRSTUV]{3}[A-Z]$`)
	nirCorsicaA = regexp.MustCompile(`^(\d{5})2A`)
	nirCorsicaB = regexp.MustCompile(`^(\d{5})2B`)
)

func idCardDe(value string) bool {
	compact := compactOf(value)
	return idCardShape.MatchString(compact) && icaoCheck(compact[:9]) == int(compact[9]-'0')
}

// mrzLine is the second line of a passport MRZ: document number, birth date and
// expiry each carry their own check digit, and one more checks them all.
func mrzLine(value string) bool {
	line := strings.TrimSpace(value)
	if len(line) != 44 {
		return false
	}
	field := func(start, length int) bool {
		return icaoCheck(line[start:start+length]) == int(line[start+length]-'0')
	}
	composite := line[0:10] + line[13:20] + line[21:43]
	return field(0, 9) && field(13, 6) && field(21, 6) && icaoCheck(composite) == int(line[43]-'0')
}

// ahv is the Swiss AHV number: an EAN-13 starting with 756.
func ahv(value string) bool {
	digits := digitsOf(value)
	if !ahvShape.MatchString(digits) {
		return false
	}
	sum := 0
	for index := 0; index < 12; index++ {
		factor := 1
		if index%2 == 1 {
			factor = 3
		}
		sum += int(digits[index]-'0') * factor
	}
	return (10-(sum%10))%10 == int(digits[12]-'0')
}

// svnrAt is the Austrian social security number: the fourth digit checks the
// other nine.
func svnrAt(value string) bool {
	digits := digitsOf(value)
	if !svnrShape.MatchString(digits) || digits[0] == '0' {
		return false
	}
	weights := []int{3, 7, 9, 0, 5, 8, 4, 2, 1, 6}
	sum := 0
	for index, weight := range weights {
		sum += weight * int(digits[index]-'0')
	}
	return sum%11 == int(digits[3]-'0')
}

// nirFr is the French social security number (NIR): 13 digits and a mod-97
// key; Corsica (2A/2B) counts as 19/18.
func nirFr(value string) bool {
	compact := compactOf(value)
	compact = nirCorsicaA.ReplaceAllString(compact, "${1}19")
	compact = nirCorsicaB.ReplaceAllString(compact, "${1}18")
	if !nirShape.MatchString(compact) {
		return false
	}
	base, err := strconv.ParseInt(compact[:13], 10, 64)
	if err != nil {
		return false
	}
	check, err := strconv.Atoi(compact[13:])
	if err != nil {
		return false
	}
	return 97-int(base%97) == check
}

// dniEs is the Spanish DNI and NIE: the letter is the number modulo 23.
func dniEs(value string) bool {
	compact := compactOf(value)
	if strings.HasPrefix(compact, "X") {
		compact = "0" + compact[1:]
	} else if strings.HasPrefix(compact, "Y") {
		compact = "1" + compact[1:]
	} else if strings.HasPrefix(compact, "Z") {
		compact = "2" + compact[1:]
	}
	match := dniShape.FindStringSubmatch(compact)
	if match == nil {
		return false
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return false
	}
	const letters = "TRWAGMYFPDXBNJZSQVHLCKE"
	return string(letters[number%23]) == match[2]
}

var cfOdd = []int{1, 0, 5, 7, 9, 13, 15, 17, 19, 21, 2, 4, 18, 20, 11, 3, 6, 8, 12, 14, 16, 10, 22, 25, 24, 23}

// codiceFiscale is the Italian tax code: the last letter checks the first
// fifteen characters.
func codiceFiscale(value string) bool {
	compact := compactOf(value)
	if !fiscalShape.MatchString(compact) {
		return false
	}
	sum := 0
	for index := 0; index < 15; index++ {
		char := compact[index]
		position := 0
		if char >= '0' && char <= '9' {
			position = int(char - '0')
		} else {
			position = int(char) - 65
		}
		if index%2 == 0 {
			sum += cfOdd[position]
		} else {
			sum += position
		}
	}
	return byte(65+sum%26) == compact[15]
}

// bsnNl is the Dutch BSN, the "11-proef".
func bsnNl(value string) bool {
	digits := digitsOf(value)
	if !bsnShape.MatchString(digits) {
		return false
	}
	sum := 0
	for index := 0; index < 8; index++ {
		sum += int(digits[index]-'0') * (9 - index)
	}
	sum -= int(digits[8] - '0')
	return sum%11 == 0 && sum > 0
}

func peselPl(value string) bool {
	digits := digitsOf(value)
	if !peselShape.MatchString(digits) {
		return false
	}
	weights := []int{1, 3, 7, 9, 1, 3, 7, 9, 1, 3}
	sum := 0
	for index, weight := range weights {
		sum += weight * int(digits[index]-'0')
	}
	return (10-(sum%10))%10 == int(digits[10]-'0')
}

// nationalNumberBe is the Belgian national number: mod 97 over the first nine
// digits (with a leading 2 for people born in 2000 or later).
func nationalNumberBe(value string) bool {
	digits := digitsOf(value)
	if !beShape.MatchString(digits) {
		return false
	}
	check, err := strconv.Atoi(digits[9:])
	if err != nil {
		return false
	}
	base, err := strconv.ParseInt(digits[:9], 10, 64)
	if err != nil {
		return false
	}
	return 97-int(base%97) == check || 97-int((2_000_000_000+base)%97) == check
}

func nhsUk(value string) bool {
	digits := digitsOf(value)
	if !nhsShape.MatchString(digits) {
		return false
	}
	sum := 0
	for index := 0; index < 9; index++ {
		sum += int(digits[index]-'0') * (10 - index)
	}
	check := 11 - (sum % 11)
	if check == 10 {
		return false
	}
	if check == 11 {
		check = 0
	}
	return check == int(digits[9]-'0')
}

// abaRouting is the US bank routing number (ABA), weights 3-7-1.
func abaRouting(value string) bool {
	digits := digitsOf(value)
	if !abaShape.MatchString(digits) {
		return false
	}
	weights := []int{3, 7, 1}
	sum := 0
	for index := 0; index < len(digits); index++ {
		sum += int(digits[index]-'0') * weights[index%3]
	}
	return sum%10 == 0
}

var vinValues = map[byte]int{
	'A': 1, 'B': 2, 'C': 3, 'D': 4, 'E': 5, 'F': 6, 'G': 7, 'H': 8, 'J': 1, 'K': 2, 'L': 3, 'M': 4,
	'N': 5, 'P': 7, 'R': 9, 'S': 2, 'T': 3, 'U': 4, 'V': 5, 'W': 6, 'X': 7, 'Y': 8, 'Z': 9,
}

var vinWeights = []int{8, 7, 6, 5, 4, 3, 2, 10, 0, 9, 8, 7, 6, 5, 4, 3, 2}

// vinCheck is the North American vehicle identification number check digit
// (position 9).
func vinCheck(value string) bool {
	vin := strings.ToUpper(value)
	if !vinShape.MatchString(vin) {
		return false
	}
	sum := 0
	for index := 0; index < len(vin); index++ {
		char := vin[index]
		number := 0
		if char >= '0' && char <= '9' {
			number = int(char - '0')
		} else {
			number = vinValues[char]
		}
		sum += number * vinWeights[index]
	}
	check := sum % 11
	want := strconv.Itoa(check)
	if check == 10 {
		want = "X"
	}
	return want == string(vin[8])
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Decode decodes a base58 payload; nil when a character is not in the
// alphabet.
func base58Decode(value string) []byte {
	var number []byte // big-endian
	for index := 0; index < len(value); index++ {
		digit := strings.IndexByte(base58Alphabet, value[index])
		if digit < 0 {
			return nil
		}
		carry := digit
		for position := len(number) - 1; position >= 0; position-- {
			carry += int(number[position]) * 58
			number[position] = byte(carry % 256)
			carry /= 256
		}
		for carry > 0 {
			number = append([]byte{byte(carry % 256)}, number...)
			carry /= 256
		}
	}
	for index := 0; index < len(value); index++ {
		if value[index] != '1' {
			break
		}
		number = append([]byte{0}, number...)
	}
	return number
}

// base58Check is a Bitcoin, Litecoin, Tron and other base58check address.
func base58Check(value string) bool {
	bytes := base58Decode(value)
	if len(bytes) < 21 {
		return false
	}
	payload := bytes[:len(bytes)-4]
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	want := bytes[len(bytes)-4:]
	for index := 0; index < 4; index++ {
		if want[index] != second[index] {
			return false
		}
	}
	return true
}

const bech32Alphabet = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

var bech32Generator = []uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

// bech32 is a Bech32 or Bech32m address (bc1…, ltc1…, tb1…).
func bech32(value string) bool {
	lower := strings.ToLower(value)
	separator := strings.LastIndexByte(lower, '1')
	if separator < 1 || len(lower)-separator < 7 {
		return false
	}
	hrp := lower[:separator]
	data := make([]int, 0, len(lower)-separator-1)
	for index := separator + 1; index < len(lower); index++ {
		digit := strings.IndexByte(bech32Alphabet, lower[index])
		if digit < 0 {
			return false
		}
		data = append(data, digit)
	}
	numbers := make([]uint32, 0, len(hrp)*2+1+len(data))
	for index := 0; index < len(hrp); index++ {
		numbers = append(numbers, uint32(hrp[index])>>5)
	}
	numbers = append(numbers, 0)
	for index := 0; index < len(hrp); index++ {
		numbers = append(numbers, uint32(hrp[index])&31)
	}
	for _, digit := range data {
		numbers = append(numbers, uint32(digit))
	}
	check := uint32(1)
	for _, number := range numbers {
		top := check >> 25
		check = ((check & 0x1ffffff) << 5) ^ number
		for bit := 0; bit < 5; bit++ {
			if (top>>bit)&1 == 1 {
				check ^= bech32Generator[bit]
			}
		}
	}
	return check == 1 || check == 0x2bc830a3
}

var nonSpaceColon = regexp.MustCompile(`^[^\s:]{1,}:\S+$`)

func jwt(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) == 0 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[0], "="))
	if err != nil {
		return false
	}
	var header map[string]any
	if err := json.Unmarshal(raw, &header); err != nil || header == nil {
		return false
	}
	_, hasAlg := header["alg"]
	_, hasTyp := header["typ"]
	return hasAlg || hasTyp
}

// base64Lenient decodes leniently: unknown characters are skipped and padding
// is optional.
func base64Lenient(value string) []byte {
	var filtered strings.Builder
	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char >= 'A' && char <= 'Z', char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '+', char == '/', char == '-', char == '_':
			filtered.WriteByte(char)
		case char == '=':
			// padding ends the data
			index = len(value)
		}
	}
	text := filtered.String()
	if raw, err := base64.RawStdEncoding.DecodeString(text); err == nil {
		return raw
	}
	if raw, err := base64.RawURLEncoding.DecodeString(text); err == nil {
		return raw
	}
	return nil
}

func basicAuth(value string) bool {
	decoded := base64Lenient(value)
	return nonSpaceColon.MatchString(string(decoded))
}

// ipv6 requires at least three groups, so C++ scopes ("a::b") and times do not
// count, and leaves documentation addresses alone.
func ipv6(value string) bool {
	address, err := netip.ParseAddr(value)
	if err != nil || !address.IsValid() || address.Is4() {
		return false
	}
	groups := 0
	for _, part := range strings.Split(value, ":") {
		if part != "" {
			groups++
		}
	}
	return groups >= 3 && !strings.HasPrefix(strings.ToLower(value), "2001:db8:")
}

var numberLike = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// coordinates is the range check for "lat, lon" pairs.
func coordinates(value string) bool {
	numbers := numberLike.FindAllString(value, -1)
	if len(numbers) < 2 {
		return false
	}
	first, err1 := strconv.ParseFloat(numbers[0], 64)
	second, err2 := strconv.ParseFloat(numbers[1], 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return math.Abs(first) <= 90 && math.Abs(second) <= 180 && !(first == 0 && second == 0)
}

// placeholderValue matches values that only stand in for a secret.
var placeholderValue = regexp.MustCompile(`(?i)^(?:\$\{?[\w.:-]+\}?|%[\w]+%|<[^>]*>|\{\{.*\}\}|\[[A-Z]+_\d+\]|x{3,}|\*{3,}|\.{3,}|-+|_+|0+|changeme|change_me|your[-_ ]?\w*|my[-_ ]?\w*|example\w*|placeholder|dummy|sample|test|todo|tbd|null|none|nil|true|false|undefined|redacted|secret|password|passwort|token)$`)

func notPlaceholder(value string) bool {
	return !placeholderValue.MatchString(strings.TrimSpace(value))
}

var (
	mrzNameLike = regexp.MustCompile(`[A-Z]{2}<<[A-Z]`)
	lowerHex    = regexp.MustCompile(`[a-f]`)
	upperHex    = regexp.MustCompile(`[A-F]`)
	hasDigit    = regexp.MustCompile(`\d`)
	hasLetter   = regexp.MustCompile(`[A-Za-z]`)
	hasLower    = regexp.MustCompile(`[a-z]`)
	hasUpper    = regexp.MustCompile(`[A-Z]`)
	hexOnly     = regexp.MustCompile(`(?i)^[0-9a-f]+$`)
)

// mrzNames is a machine-readable zone name line: letters with "<<" between the
// surname and the given names.
func mrzNames(value string) bool { return mrzNameLike.MatchString(value) }

// mixedCaseHex is an EIP-55 checksummed Ethereum address: upper- and lowercase
// hex letters after the 0x prefix.
func mixedCaseHex(value string) bool {
	if len(value) < 2 {
		return false
	}
	rest := value[2:]
	return lowerHex.MatchString(rest) && upperHex.MatchString(rest)
}

// mixedAlnum means letters and digits together: real IDs, not words or amounts.
func mixedAlnum(value string) bool {
	return hasDigit.MatchString(value) && hasLetter.MatchString(value)
}

// tokenLike rejects hex-only values (git hashes, digests) and plain words.
func tokenLike(value string) bool {
	return !hexOnly.MatchString(value) && hasDigit.MatchString(value) && hasLower.MatchString(value) && hasUpper.MatchString(value)
}

// Validators are the named checks a rule can require, by the keys the packs use
// (`validator: 'iban'`).
var Validators = map[string]func(string) bool{
	"luhn":             func(value string) bool { return luhn(digitsOf(value)) },
	"card":             cardValid,
	"iban":             ibanValid,
	"phone":            looksLikePhone,
	"steuerId":         steuerID,
	"ustIdDe":          ustIDDe,
	"rvnr":             rvnr,
	"kvnr":             kvnr,
	"idCardDe":         idCardDe,
	"mrzLine":          mrzLine,
	"ahv":              ahv,
	"svnrAt":           svnrAt,
	"nirFr":            nirFr,
	"dniEs":            dniEs,
	"codiceFiscale":    codiceFiscale,
	"bsnNl":            bsnNl,
	"peselPl":          peselPl,
	"nationalNumberBe": nationalNumberBe,
	"nhsUk":            nhsUk,
	"abaRouting":       abaRouting,
	"vinCheck":         vinCheck,
	"base58Check":      base58Check,
	"bech32":           bech32,
	"jwt":              jwt,
	"basicAuth":        basicAuth,
	"ipv6":             ipv6,
	"coordinates":      coordinates,
	"notPlaceholder":   notPlaceholder,
	"mixedAlnum":       mixedAlnum,
	"mrzNames":         mrzNames,
	"mixedCaseHex":     mixedCaseHex,
	"tokenLike":        tokenLike,
}
