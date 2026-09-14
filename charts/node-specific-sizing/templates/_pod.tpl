{{- define "node-specific-sizing.pod" -}}
serviceAccountName: {{ include "node-specific-sizing.serviceAccountName" . }}
terminationGracePeriodSeconds: 10
containers:
    - name: {{ .Chart.Name }}
      image: {{ .Values.image.registry }}/{{ .Values.image.tag }}@sha256:{{ .Values.image.sha256 }}
      imagePullPolicy:  {{ .Values.image.pullPolicy }}
          env:
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          volumeMounts:
            - mountPath: /tmp/k8s-webhook-server/serving-certs
              name: cert
              readOnly: true
          securityContext:
            runAsNonRoot: true
      volumes:
        - name: cert
          secret:
            defaultMode: 420
            secretName: node-specific-sizing-cert
{{- end }}