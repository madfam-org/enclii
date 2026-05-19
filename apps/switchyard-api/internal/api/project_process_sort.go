package api

func sortProjectProcesses(processes []projectProcess) {
	for i := 1; i < len(processes); i++ {
		current := processes[i]
		j := i - 1
		for j >= 0 && processes[j].UpdatedAt.Before(current.UpdatedAt) {
			processes[j+1] = processes[j]
			j--
		}
		processes[j+1] = current
	}
}

func sortServiceProcessSummaries(summaries []serviceProcessSummary) {
	for i := 1; i < len(summaries); i++ {
		current := summaries[i]
		j := i - 1
		for j >= 0 && serviceSummarySortKey(summaries[j]) < serviceSummarySortKey(current) {
			summaries[j+1] = summaries[j]
			j--
		}
		summaries[j+1] = current
	}
}

func serviceSummarySortKey(summary serviceProcessSummary) int {
	return summary.BlockedCount*100 + summary.FailedCount*10 + summary.ActiveCount
}
