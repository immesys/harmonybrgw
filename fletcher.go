package main

func fletcher16(data []byte) uint16 {
	var sum1 uint16 = 0xff
	var sum2 uint16 = 0xff

	for len(data) > 0 {
		tlen := len(data)
		if tlen > 20 {
			tlen = 20
		}
		for i := 0; i < tlen; i++ {
			sum1 += uint16(data[i])
			sum2 += sum1
		}
		data = data[tlen:]
		sum1 = (sum1 & 0xff) + (sum1 >> 8)
		sum2 = (sum2 & 0xff) + (sum2 >> 8)
	}
	/* Second reduction step to reduce sums to 8 bits */
	sum1 = (sum1 & 0xff) + (sum1 >> 8)
	sum2 = (sum2 & 0xff) + (sum2 >> 8)
	return (sum2 << 8) | sum1
}
