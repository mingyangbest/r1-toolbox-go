package hardware

func (f *FanController) GetTempPublic(sensor string) float64 {
	return f.GetTemp(sensor)
}
