package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleQuestionReply(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("requestID")
	if requestID == "" {
		s.httpError(w, "Missing request ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.httpError(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	countStr := r.FormValue("question_count")
	questionCount, err := strconv.Atoi(countStr)
	if err != nil || questionCount < 1 {
		questionCount = 1
	}

	var answers [][]string
	var summaryParts []string

	for qi := 0; qi < questionCount; qi++ {
		key := fmt.Sprintf("q%d", qi)
		values := r.Form[key]
		customKey := fmt.Sprintf("q%d_custom", qi)
		customValue := strings.TrimSpace(r.FormValue(customKey))

		var answer []string
		for _, v := range values {
			if v == "__custom__" {
				if customValue != "" {
					answer = append(answer, customValue)
				}
			} else {
				answer = append(answer, v)
			}
		}
		if answer == nil {
			answer = []string{}
		}

		answers = append(answers, answer)
		summaryParts = append(summaryParts, strings.Join(answer, ", "))
	}

	// Proxy reply to OpenCode
	payload := map[string]any{"answers": answers}
	jsonData, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/question/%s/reply", s.Sandbox.OpencodeURL(), requestID)
	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		log.Printf("handleQuestionReply: failed to proxy reply: %v", err)
		s.httpError(w, "Failed to send reply", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("handleQuestionReply: OpenCode returned %d for request %s", resp.StatusCode, requestID)
	}

	summary := strings.Join(summaryParts, "; ")
	if summary == "" {
		summary = "(no selection)"
	}

	data := struct{ Summary string }{Summary: "Answered: " + summary}
	s.renderHTML(w, "question-answered", data)
}

func (s *Server) handleQuestionReject(w http.ResponseWriter, r *http.Request) {
	requestID := r.PathValue("requestID")
	if requestID == "" {
		s.httpError(w, "Missing request ID", http.StatusBadRequest)
		return
	}

	url := fmt.Sprintf("%s/question/%s/reject", s.Sandbox.OpencodeURL(), requestID)
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		log.Printf("handleQuestionReject: failed to proxy reject: %v", err)
		s.httpError(w, "Failed to send rejection", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	s.renderHTML(w, "question-dismissed", nil)
}
